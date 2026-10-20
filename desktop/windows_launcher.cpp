#define NOMINMAX
#include <windows.h>
#include <shellapi.h>
#include <shlobj.h>
#include <string>
#include <vector>

static std::wstring sensorBinary(){
 PWSTR directory=nullptr;if(FAILED(SHGetKnownFolderPath(FOLDERID_ProgramFiles,0,nullptr,&directory)))return {};
 std::wstring result=std::wstring(directory)+L"\\SharkCoolSensors\\sharkcool-sensors.exe";CoTaskMemFree(directory);return result;
}
static bool sensorsReady(){
 SC_HANDLE manager=OpenSCManagerW(nullptr,nullptr,SC_MANAGER_CONNECT);if(!manager)return false;
 SC_HANDLE service=OpenServiceW(manager,L"SharkCoolSensors",SERVICE_QUERY_STATUS);CloseServiceHandle(manager);if(!service)return false;
 SERVICE_STATUS_PROCESS status{};DWORD needed=0;
 bool running=QueryServiceStatusEx(service,SC_STATUS_PROCESS_INFO,reinterpret_cast<BYTE*>(&status),sizeof(status),&needed)&&status.dwCurrentState==SERVICE_RUNNING;CloseServiceHandle(service);if(!running)return false;
 auto binary=sensorBinary();DWORD ignored=0,size=GetFileVersionInfoSizeW(binary.c_str(),&ignored);if(!size)return false;
 std::vector<BYTE> data(size);VS_FIXEDFILEINFO *version=nullptr;UINT length=0;
 return GetFileVersionInfoW(binary.c_str(),0,size,data.data())&&VerQueryValueW(data.data(),L"\\",reinterpret_cast<void**>(&version),&length)&&length>=sizeof(*version)&&version->dwSignature==0xfeef04bd&&(version->dwFileVersionMS>MAKELONG(SHARKCOOL_VERSION_MINOR,SHARKCOOL_VERSION_MAJOR)||(version->dwFileVersionMS==MAKELONG(SHARKCOOL_VERSION_MINOR,SHARKCOOL_VERSION_MAJOR)&&version->dwFileVersionLS>=MAKELONG(0,SHARKCOOL_VERSION_PATCH)));
}
static bool initializeSensors(const std::wstring &folder){
 if(sensorsReady())return true;
 auto script=folder+L"\\Install-Sensors.ps1",source=folder+L"\\sensors";
 if(GetFileAttributesW((source+L"\\sharkcool-sensors.exe").c_str())==INVALID_FILE_ATTRIBUTES){auto binary=sensorBinary();source=binary.substr(0,binary.find_last_of(L"\\/"));}
 if(GetFileAttributesW(script.c_str())==INVALID_FILE_ATTRIBUTES||GetFileAttributesW((source+L"\\sharkcool-sensors.exe").c_str())==INVALID_FILE_ATTRIBUTES)return false;
 wchar_t system[32768];UINT count=GetSystemDirectoryW(system,32768);if(!count||count>=32768)return false;
 std::wstring powershell=std::wstring(system)+L"\\WindowsPowerShell\\v1.0\\powershell.exe";
 std::wstring parameters=L"-NoProfile -NonInteractive -ExecutionPolicy Bypass -File \""+script+L"\" -Source \""+source+L"\"";
 SHELLEXECUTEINFOW request{};request.cbSize=sizeof(request);request.fMask=SEE_MASK_NOCLOSEPROCESS|SEE_MASK_NOASYNC;request.lpVerb=L"runas";request.lpFile=powershell.c_str();request.lpParameters=parameters.c_str();request.nShow=SW_HIDE;
 if(!ShellExecuteExW(&request))return false;
 DWORD code=1;bool complete=WaitForSingleObject(request.hProcess,120000)==WAIT_OBJECT_0&&GetExitCodeProcess(request.hProcess,&code)&&code==0;CloseHandle(request.hProcess);return complete&&sensorsReady();
}
static bool prepareSensors(const std::wstring &folder){
 if(sensorsReady())return true;
 HANDLE lock=CreateMutexW(nullptr,FALSE,L"Local\\SharkCool-Sensors-Setup");if(!lock)return false;
 DWORD state=WaitForSingleObject(lock,120000);bool result=false;
 if(state==WAIT_OBJECT_0||state==WAIT_ABANDONED){result=initializeSensors(folder);ReleaseMutex(lock);}CloseHandle(lock);return result;
}
int WINAPI wWinMain(HINSTANCE,HINSTANCE,PWSTR arguments,int){
 wchar_t filename[32768];DWORD count=GetModuleFileNameW(nullptr,filename,32768);if(count==0||count>=32768)return 1;
 std::wstring folder(filename,count);folder.resize(folder.find_last_of(L"\\/"));
 int argc=0;LPWSTR *argv=CommandLineToArgvW(GetCommandLineW(),&argc);if(!argv)return 1;
 bool gui=argc==1||(argc>1&&std::wstring(argv[1])==L"gui"),demo=false,tray=false,help=false;
 for(int i=1;i<argc;i++){std::wstring arg=argv[i];demo|=arg==L"--demo"||arg==L"-demo"||arg.rfind(L"--demo=",0)==0||arg.rfind(L"-demo=",0)==0;tray|=arg==L"--tray"||arg==L"-tray"||arg.rfind(L"--tray=",0)==0||arg.rfind(L"-tray=",0)==0;help|=arg==L"--help"||arg==L"-help"||arg==L"-h";}
 LocalFree(argv);
 // Login launches must never trigger UAC; initialization belongs to an explicit app launch.
 if(gui&&!demo&&!tray&&!help&&!prepareSensors(folder))MessageBoxW(nullptr,L"温度采集初始化未完成，部分温度可能无法显示。散热器控制仍可使用；再次打开应用会重试初始化。",L"SharkCool",MB_OK|MB_ICONINFORMATION);
 std::wstring executable=folder+L"\\sharkcool-service.exe";
 std::wstring command=L"\""+executable+L"\" "+(arguments&&*arguments?arguments:L"gui");STARTUPINFOW startup{};startup.cb=sizeof(startup);PROCESS_INFORMATION process{};
 if(!CreateProcessW(executable.c_str(),command.data(),nullptr,nullptr,FALSE,CREATE_NO_WINDOW,nullptr,folder.c_str(),&startup,&process)){MessageBoxW(nullptr,L"无法启动 SharkCool 服务。请完整解压应用包，并保留所有程序和 DLL 文件。",L"SharkCool",MB_OK|MB_ICONERROR);return 1;}
 CloseHandle(process.hThread);CloseHandle(process.hProcess);return 0;
}
