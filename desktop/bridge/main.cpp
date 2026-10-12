// Windows HID + BLE adapter. Device protocol and recovery live in the Go driver.
#include <QCoreApplication>
#include <QBluetoothDeviceDiscoveryAgent>
#include <QBluetoothLocalDevice>
#include <QLowEnergyController>
#include <QLowEnergyService>
#include <QJsonDocument>
#include <QJsonArray>
#include <QJsonObject>
#include <vector>
#include <functional>
#include <QTimer>
#include <QRegularExpression>
#include <QByteArray>
#include <cstdio>
#include <iostream>
#include <thread>
#include <mutex>
#include <atomic>
#include <memory>
#ifdef Q_OS_WIN
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <windows.h>
#include <setupapi.h>
#include <hidsdi.h>
#include <hidpi.h>
#endif

static std::mutex outputMutex;
static void output(const QJsonObject &message){const auto bytes=QJsonDocument(message).toJson(QJsonDocument::Compact)+"\n";std::lock_guard<std::mutex> guard(outputMutex);std::fwrite(bytes.constData(),1,size_t(bytes.size()),stdout);std::fflush(stdout);}
static void failure(const QString &message){output({{"type","error"},{"error",message}});QCoreApplication::exit(1);}
static bool report(const QJsonObject &message,QByteArray &bytes){
 const auto data=message.value("data").toString();if(message.value("type")!="write"||message.value("id").toInt()<=0||data.size()!=130||!QRegularExpression("^[0-9a-f]{130}$").match(data).hasMatch())return false;
 bytes=QByteArray::fromHex(data.toLatin1());if(bytes.size()!=65||bytes[0]!=0||(quint8(bytes[1])!=0xa5&&quint8(bytes[1])!=0xa4))return false;
 int length=quint8(bytes[2]);if(length<(quint8(bytes[1])==0xa4?6:4)||length>64)return false;quint8 sum=0;for(int i=1;i<length;i++)sum+=quint8(bytes[i]);if(sum!=quint8(bytes[length]))return false;if(quint8(bytes[1])==0xa4&&((quint8(bytes[3])|(quint8(bytes[4])<<8))&0x7fff)==0)return false;return true;
}
static void inputThread(QObject *owner,std::function<void(QJsonObject)> write){
 std::thread([owner,write]{std::string line;while(std::getline(std::cin,line)){
  if(line.size()>8192){QMetaObject::invokeMethod(owner,[]{failure("IPC request too large");});return;}
  QJsonParseError error;auto document=QJsonDocument::fromJson(QByteArray::fromStdString(line),&error);if(error.error!=QJsonParseError::NoError||!document.isObject()){QMetaObject::invokeMethod(owner,[]{failure("Invalid IPC request");});return;}
  QMetaObject::invokeMethod(owner,[write,message=document.object()]{write(message);});
 }QMetaObject::invokeMethod(owner,[]{QCoreApplication::quit();});}).detach();
}
#ifdef Q_OS_WIN
static QString winError(const char *what,DWORD code=GetLastError()){return QString("%1 (Windows error %2)").arg(QString::fromLatin1(what)).arg(code);}
struct HidEndpoint{QString path,name;USHORT inputLength=0,outputLength=0;};
static QList<HidEndpoint> hidEndpoints(){
 QList<HidEndpoint> result;GUID guid;HidD_GetHidGuid(&guid);HDEVINFO set=SetupDiGetClassDevsW(&guid,nullptr,nullptr,DIGCF_PRESENT|DIGCF_DEVICEINTERFACE);if(set==INVALID_HANDLE_VALUE)return result;
 for(DWORD i=0;;i++){
  SP_DEVICE_INTERFACE_DATA entry{};entry.cbSize=sizeof(entry);if(!SetupDiEnumDeviceInterfaces(set,nullptr,&guid,i,&entry))break;
  DWORD required=0;SetupDiGetDeviceInterfaceDetailW(set,&entry,nullptr,0,&required,nullptr);if(required<sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W)||required>65536)continue;
  std::vector<unsigned char> storage(required);auto detail=reinterpret_cast<SP_DEVICE_INTERFACE_DETAIL_DATA_W*>(storage.data());detail->cbSize=sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W);if(!SetupDiGetDeviceInterfaceDetailW(set,&entry,detail,required,nullptr,nullptr))continue;
  HANDLE device=CreateFileW(detail->DevicePath,0,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,0,nullptr);if(device==INVALID_HANDLE_VALUE)continue;
  HIDD_ATTRIBUTES attributes{};attributes.Size=sizeof(attributes);PHIDP_PREPARSED_DATA data=nullptr;HIDP_CAPS caps{};
  if(HidD_GetAttributes(device,&attributes)&&attributes.VendorID==0xe2b7&&attributes.ProductID==0x7001&&HidD_GetPreparsedData(device,&data)){
   if(HidP_GetCaps(data,&caps)==HIDP_STATUS_SUCCESS&&caps.InputReportByteLength==65&&caps.OutputReportByteLength==65){wchar_t name[256]{};HidD_GetProductString(device,name,sizeof(name));result.append({QString::fromWCharArray(detail->DevicePath),name[0]?QString::fromWCharArray(name):"Black Shark BRB02",caps.InputReportByteLength,caps.OutputReportByteLength});}HidD_FreePreparsedData(data);
  }CloseHandle(device);
 }SetupDiDestroyDeviceInfoList(set);return result;
}
class HidConnection{
 HANDLE handle=INVALID_HANDLE_VALUE;std::thread reader;std::atomic_bool stopped{false};QObject *owner;HidEndpoint endpoint;
 bool transfer(bool write,QByteArray &bytes,DWORD timeout){
  OVERLAPPED operation{};operation.hEvent=CreateEventW(nullptr,TRUE,FALSE,nullptr);if(!operation.hEvent){failure(winError("CreateEvent"));return false;}
  DWORD count=0;BOOL ok=write?WriteFile(handle,bytes.data(),DWORD(bytes.size()),&count,&operation):ReadFile(handle,bytes.data(),DWORD(bytes.size()),&count,&operation);
  DWORD error=ok?ERROR_SUCCESS:GetLastError();if(!ok&&error==ERROR_IO_PENDING){DWORD wait=WaitForSingleObject(operation.hEvent,timeout);if(wait!=WAIT_OBJECT_0){CancelIoEx(handle,&operation);GetOverlappedResult(handle,&operation,&count,TRUE);error=wait==WAIT_TIMEOUT?ERROR_TIMEOUT:GetLastError();}else if(!GetOverlappedResult(handle,&operation,&count,FALSE))error=GetLastError();else error=ERROR_SUCCESS;}
  CloseHandle(operation.hEvent);if(error!=ERROR_SUCCESS||count!=DWORD(bytes.size())){if(!stopped){auto message=winError(write?"HID write":"HID read",error);QMetaObject::invokeMethod(owner,[message]{failure(message);});}return false;}return true;
 }
public:
 explicit HidConnection(QObject *parent):owner(parent){}
 bool open(const QString &target){for(const auto &candidate:hidEndpoints()){if(!target.isEmpty()&&candidate.path.compare(target,Qt::CaseInsensitive)!=0)continue;
  handle=CreateFileW(reinterpret_cast<LPCWSTR>(candidate.path.utf16()),GENERIC_READ|GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,FILE_FLAG_OVERLAPPED,nullptr);if(handle==INVALID_HANDLE_VALUE)continue;endpoint=candidate;
  reader=std::thread([this]{while(!stopped){QByteArray bytes(endpoint.inputLength,0);if(!transfer(false,bytes,INFINITE))break;if(!stopped)output({{"type","packet"},{"data",QString::fromLatin1(bytes.toHex())}});}});
  output({{"type","ready"},{"device",endpoint.path}});return true;
 }failure("No writable BRB02 HID interface E2B7:7001 with 65-byte reports; check USB-IN cable and close competing drivers");return false;}
 void write(const QJsonObject &message){QByteArray bytes;if(!report(message,bytes)){failure("Invalid HID report");return;}if(transfer(true,bytes,2500))output({{"type","written"},{"id",message.value("id")}});}
 ~HidConnection(){stopped=true;if(handle!=INVALID_HANDLE_VALUE)CancelIoEx(handle,nullptr);if(reader.joinable())reader.join();if(handle!=INVALID_HANDLE_VALUE)CloseHandle(handle);}
};
#endif
class BleConnection : public QObject{
 QBluetoothDeviceDiscoveryAgent scan;
 QBluetoothLocalDevice local;
 QLowEnergyController *controller=nullptr;
 QLowEnergyService *service=nullptr;
 QLowEnergyCharacteristic notify,writer;
 QLowEnergyDescriptor descriptor;
 QBluetoothDeviceInfo device;
 bool ready=false,listing=false,connecting=false,notifyMode=true;
 int pendingWrite=0;
 QJsonArray devices;
 QString target;
 const QBluetoothUuid serviceID{QString("0000ae30-0000-1000-8000-00805f9b34fb")},notifyID{QString("0000ae04-0000-1000-8000-00805f9b34fb")},writeID{QString("0000ae10-0000-1000-8000-00805f9b34fb")};
 void connectDevice(const QBluetoothDeviceInfo &info){
  if(connecting)return;connecting=true;scan.stop();device=info;
  if(local.pairingStatus(info.address())==QBluetoothLocalDevice::Unpaired){failure("Pair BS BRB02 Cooler Pro in Windows Bluetooth settings first");return;}
  controller=QLowEnergyController::createCentral(info,this);
  connect(controller,&QLowEnergyController::errorOccurred,this,[this](auto){failure("BLE: "+controller->errorString());});
  connect(controller,&QLowEnergyController::disconnected,this,[]{failure("BRB02 BLE disconnected");});
  connect(controller,&QLowEnergyController::connected,this,[this]{controller->discoverServices();});
  connect(controller,&QLowEnergyController::discoveryFinished,this,[this]{
   service=controller->createServiceObject(serviceID,this);if(!service){failure("BRB02 AE30 service missing");return;}
   connect(service,&QLowEnergyService::errorOccurred,this,[](auto code){failure(QString("BRB02 GATT error %1").arg(int(code)));});
   connect(service,&QLowEnergyService::stateChanged,this,[this](auto state){
    if(state==QLowEnergyService::InvalidService){failure("BRB02 GATT service invalidated");return;}
    if(state!=QLowEnergyService::RemoteServiceDiscovered)return;
    notify=service->characteristic(notifyID);writer=service->characteristic(writeID);
    if(!notify.isValid()||!writer.isValid()||!(notify.properties()&(QLowEnergyCharacteristic::Notify|QLowEnergyCharacteristic::Indicate))||!(writer.properties()&(QLowEnergyCharacteristic::Write|QLowEnergyCharacteristic::WriteNoResponse))){failure("BRB02 AE04/AE10 characteristics unavailable");return;}
    descriptor=notify.descriptor(QBluetoothUuid::DescriptorType::ClientCharacteristicConfiguration);if(!descriptor.isValid()){failure("BRB02 notification descriptor missing");return;}
    notifyMode=notify.properties().testFlag(QLowEnergyCharacteristic::Notify);service->writeDescriptor(descriptor,QByteArray::fromHex(notifyMode?"0100":"0200"));
   });
   connect(service,&QLowEnergyService::descriptorWritten,this,[this](const auto &written,const QByteArray &value){if(written==descriptor&&value==QByteArray::fromHex(notifyMode?"0100":"0200")){ready=true;output({{"type","ready"},{"device","ble://"+device.address().toString()}});}});
   connect(service,&QLowEnergyService::characteristicChanged,this,[this](const auto &characteristic,const QByteArray &value){if(characteristic==notify&&!value.isEmpty())output({{"type","packet"},{"data",QString::fromLatin1(value.toHex())}});});
   connect(service,&QLowEnergyService::characteristicWritten,this,[this](const auto &characteristic,const QByteArray &){if(characteristic==writer&&pendingWrite){output({{"type","written"},{"id",pendingWrite}});pendingWrite=0;}});
   service->discoverDetails(QLowEnergyService::SkipValueDiscovery);
  });controller->connectToDevice();
 }
public:
 explicit BleConnection(bool list,const QString &address):listing(list),target(address){
  scan.setLowEnergyDiscoveryTimeout(3500);
  connect(&scan,&QBluetoothDeviceDiscoveryAgent::deviceDiscovered,this,[this](const QBluetoothDeviceInfo &info){
   if(info.name()!="BS BRB02 Cooler Pro"||!info.coreConfigurations().testFlag(QBluetoothDeviceInfo::LowEnergyCoreConfiguration))return;
   const auto address=info.address().toString();const bool paired=local.pairingStatus(info.address())!=QBluetoothLocalDevice::Unpaired;
   if(listing){for(const auto &item:devices)if(item.toObject().value("address")==address)return;devices.append(QJsonObject{{"path",address},{"address",address},{"name",info.name()},{"paired",paired},{"connected",local.connectedDevices().contains(info.address())},{"servicesResolved",false},{"controlReady",false}});}
   else if(paired&&(target.isEmpty()||target.compare(address,Qt::CaseInsensitive)==0))connectDevice(info);
  });
  connect(&scan,&QBluetoothDeviceDiscoveryAgent::errorOccurred,this,[this](auto){failure("BLE discovery: "+scan.errorString());});
  connect(&scan,&QBluetoothDeviceDiscoveryAgent::finished,this,[this]{if(listing){auto bytes=QJsonDocument(devices).toJson(QJsonDocument::Compact);std::fwrite(bytes.constData(),1,size_t(bytes.size()),stdout);std::fflush(stdout);QCoreApplication::quit();}else if(!connecting)failure("No paired BS BRB02 Cooler Pro found");});
  QTimer::singleShot(0,this,[this]{
   if(!listing&&!target.isEmpty()){QBluetoothAddress address(target);if(address.isNull()){failure("Invalid Bluetooth address");return;}QBluetoothDeviceInfo info(address,"BS BRB02 Cooler Pro",0);info.setCoreConfigurations(QBluetoothDeviceInfo::LowEnergyCoreConfiguration);connectDevice(info);}else scan.start(QBluetoothDeviceDiscoveryAgent::LowEnergyMethod);
  });
  if(!listing)QTimer::singleShot(15000,this,[this]{if(!ready)failure("BRB02 BLE connection timed out");});
 }
 void write(const QJsonObject &message){QByteArray bytes;if(!ready||pendingWrite||!report(message,bytes)){failure("Invalid BLE write or GATT not ready");return;}
  // Strip report ID and HID padding. BLE AE10 takes the length-delimited packet.
  int length=quint8(bytes[2]);QByteArray packet=bytes.mid(1,length);if(packet.size()>controller->mtu()-3){failure("BRB02 BLE MTU too small for this command");return;}
  pendingWrite=message.value("id").toInt();const bool response=writer.properties().testFlag(QLowEnergyCharacteristic::Write);
  service->writeCharacteristic(writer,packet,response?QLowEnergyService::WriteWithResponse:QLowEnergyService::WriteWithoutResponse);
  // WriteWithoutResponse has no ATT acknowledgement. The Go controller still
  // waits for the cooler's protocol ACK; never manufacture a device success.
  if(!response){output({{"type","written"},{"id",pendingWrite}});pendingWrite=0;}
 }
};
int main(int argc,char **argv){
 QCoreApplication app(argc,argv);auto args=app.arguments();
 if(args.contains("--self-test")){QByteArray bytes;QJsonObject good{{"type","write"},{"id",1},{"data",QString::fromLatin1((QByteArray::fromHex("00a5052401cf")+QByteArray(59,0)).toHex())}};if(!report(good,bytes))return 1;good.insert("data","00");if(report(good,bytes))return 1;return 0;}
 auto option=[&](const QString &name){int i=args.indexOf(name);return i>=0&&i+1<args.size()?args[i+1]:QString();};const auto list=option("--list"),channel=option("--transport"),target=option("--target");
#ifdef Q_OS_WIN
 if(list=="usb"){QJsonArray devices;for(const auto &entry:hidEndpoints())devices.append(QJsonObject{{"path",entry.path},{"name",entry.name},{"readable",true},{"writable",true}});auto bytes=QJsonDocument(devices).toJson(QJsonDocument::Compact);std::fwrite(bytes.constData(),1,size_t(bytes.size()),stdout);return 0;}
 if(channel=="usb"){HidConnection connection(&app);if(!connection.open(target))return 1;inputThread(&app,[&](QJsonObject message){connection.write(message);});return app.exec();}
#endif
 if(channel=="ble"||list=="ble"){BleConnection connection(list=="ble",target);if(list.isEmpty())inputThread(&connection,[&](QJsonObject message){connection.write(message);});return app.exec();}
 failure("Expected --transport usb|ble or --list usb|ble");return 1;
}
