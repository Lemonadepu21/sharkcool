#include "window.h"
#include <QtTest>
#include <QTcpServer>
#include <QTcpSocket>
#include <QJsonDocument>
#include <QPushButton>
#include <QSpinBox>
#include <QComboBox>
#include <QCheckBox>
#include <QLabel>
#include <QFile>
#include <QSystemTrayIcon>
#include <QAction>

class Fixture : public QTcpServer {
public:
    QString token="aabbcc";
    bool connected=true,lcd=true,rgbEnabled=true,failSwitch=false,missingRGBState=false,running=false,failDevice=false,autostart=false;
    int generation=1,rootReads=0;
    QJsonObject cooling{{"source",1},{"slot",1},{"rpm",1500}};
    QJsonObject rgb{{"effect",8},{"interval",500},{"brightness",80},{"colorCount",10},{"color",QJsonArray{0,255,72}}};
    QVector<QPair<QString,QJsonObject>> writes;
    Fixture(){
        connect(this,&QTcpServer::newConnection,this,[this]{while(hasPendingConnections()){auto s=nextPendingConnection();auto buffer=std::make_shared<QByteArray>();connect(s,&QTcpSocket::readyRead,this,[this,s,buffer]{buffer->append(s->readAll());int end=buffer->indexOf("\r\n\r\n");if(end<0)return;auto header=buffer->left(end);int len=0;for(auto line:header.split('\n'))if(line.toLower().startsWith("content-length:"))len=line.mid(15).trimmed().toInt();if(buffer->size()<end+4+len)return;auto parts=header.split(' ');QString path=QString::fromUtf8(parts.value(1));bool post=parts.value(0)=="POST";QJsonObject body=QJsonDocument::fromJson(buffer->mid(end+4,len)).object();bool authorized=header.toLower().contains("x-sharkcool-token: "+token.toUtf8());QByteArray response;int status=200;
            if(path=="/"){rootReads++;response="<meta name=\"sharkcool-token\" content=\""+token.toUtf8()+"\">";}
            else if(post&&!authorized){status=403;response=QJsonDocument(QJsonObject{{"error","缺少有效的会话令牌"}}).toJson();}
            else {auto data=handle(path,post,body,status);response=QJsonDocument(data).toJson(QJsonDocument::Compact);}
            s->write("HTTP/1.1 "+QByteArray::number(status)+" OK\r\nContent-Type: application/json\r\nConnection: close\r\nContent-Length: "+QByteArray::number(response.size())+"\r\n\r\n"+response);s->disconnectFromHost();buffer->clear();});connect(s,&QTcpSocket::disconnected,s,&QObject::deleteLater);}});
        if(!listen(QHostAddress::LocalHost,0))qFatal("fixture listen failed");
    }
    QUrl url()const{return QUrl(QString("http://127.0.0.1:%1").arg(serverPort()));}
    QJsonObject handle(const QString &path,bool post,const QJsonObject &body,int &status){
        if(path=="/api/status")return {{"connected",connected},{"stale",false},{"demo",false},{"session","test"},{"device","fixture"},{"transport","usb"},{"connectionGeneration",generation},{"rpm",1600},{"temps",QJsonObject{{"cpu",45},{"gpu",36}}}};
        if(path=="/api/screensaver")return {{"running",running},{"draftId",""},{"canRestore",false},{"progress",running?-1:0},{"phase",running?"备份原图":""}};
        if(path=="/api/settings")return {{"autostart",autostart}};
        if(path=="/api/system-info")return {{"enabled",false},{"values",QJsonObject{}}};
        if(path=="/api/query"){
            const QString kind=body.value("kind").toString();if(kind=="cooling")return {{"config",cooling}};
            if(kind=="cooling-preset"){int n=body.value("slot").toInt();const int speeds[]={1500,2134,2912,3534};return {{"config",QJsonObject{{"source",0},{"slot",n},{"rpm",speeds[n-1]}}}};}
            if(kind=="rgb")return {{"config",rgb}};
            if(kind=="rgb-switch"){if(missingRGBState)return {};return {{"enabled",rgbEnabled}};}
            if(kind=="lcd-switch")return {{"enabled",lcd}};
            if(kind=="device"){if(failDevice){status=409;return {{"error","read failed"}};}return {{"options",QJsonObject{{"smartStartStop",true},{"powerOnStart",false}}}};}
        }
        if(post)writes.append({path,body});
        if(path=="/api/autostart"){autostart=body.value("enabled").toBool();return {{"autostart",autostart}};}
        if(path=="/api/quit"){status=409;return {{"error","请等待屏保传输完成"}};}
        if(path=="/api/cooling"){cooling=body;return {{"config",body},{"active",true},{"confirmed",true}};}
        if(path=="/api/rgb"){if(body.contains("colorMode"))rgb.insert("colorCount",body.value("colorMode")=="multi"?10:1);return {{"config",rgb},{"confirmed",true}};}
        if(path=="/api/switch"){if(failSwitch){status=409;return {{"error","switch rejected"}};}if(body.value("kind")=="lcd")lcd=body.value("on").toBool();if(body.value("kind")=="rgb")rgbEnabled=body.value("on").toBool();return {{"acknowledged",true}};}
        if(path=="/api/lcd-layout")return {{"acknowledged",true}};
        status=404;return {{"error","unexpected route"}};
    }
};
class NativeWindowTests : public QObject {
    Q_OBJECT
    QPushButton *preset(MainWindow &w,int slot){for(auto b:w.findChildren<QPushButton*>())if(b->property("slot").toInt()==slot)return b;return nullptr;}
    QPushButton *named(MainWindow &w,const QString &text){for(auto b:w.findChildren<QPushButton*>())if(b->text()==text)return b;return nullptr;}
private slots:
    void initTestCase(){QFile style(":/style.qss");QVERIFY(style.open(QIODevice::ReadOnly));qApp->setStyleSheet(QString::fromUtf8(style.readAll()));}
    void traySharesAutostartAndRestoresWindow(){
        Fixture api;MainWindow w(api.url());w.enableTray(true);
        auto tray=w.findChild<QSystemTrayIcon*>("systemTray");QVERIFY(tray);QVERIFY(!tray->icon().isNull());
        auto start=w.findChild<QAction*>("trayAutostart");auto open=w.findChild<QAction*>("trayOpen");auto quit=w.findChild<QAction*>("trayExit");
        QTRY_VERIFY(start->isEnabled());QVERIFY(!start->isChecked());start->trigger();
        QTRY_COMPARE(api.writes.size(),1);QCOMPARE(api.writes[0].first,"/api/autostart");QVERIFY(api.autostart);
        QTRY_VERIFY(start->isEnabled());QVERIFY(w.findChild<QCheckBox*>("loginAutostart")->isChecked());
        w.hide();open->trigger();QVERIFY(w.isVisible());
        if(QSystemTrayIcon::isSystemTrayAvailable()){QTest::mouseClick(w.findChild<QPushButton*>("closeWindow"),Qt::LeftButton);QVERIFY(!w.isVisible());open->trigger();QVERIFY(w.isVisible());}
        else{QVERIFY(w.isVisible());w.hide();}
        quit->trigger();QTRY_COMPARE(api.writes.size(),2);QTRY_VERIFY(quit->isEnabled());QVERIFY(w.isVisible());
        QVERIFY(w.findChild<QLabel*>("feedback")->text().contains("屏保"));
    }
    void nativeIdentityAndImmediateSlots(){
        Fixture api;MainWindow w(api.url());w.show();QVERIFY(w.windowFlags().testFlag(Qt::FramelessWindowHint));QCOMPARE(w.windowTitle(),"SharkCool");QVERIFY(!w.windowIcon().isNull());
        QTRY_VERIFY_WITH_TIMEOUT(preset(w,3)->isEnabled(),5000);QTest::mouseClick(preset(w,3),Qt::LeftButton);
        QTRY_COMPARE(api.writes.size(),1);QCOMPARE(api.writes[0].first,"/api/cooling");QCOMPARE(api.writes[0].second.value("slot").toInt(),3);QTRY_VERIFY(preset(w,3)->isChecked());QCOMPARE(w.findChild<QSpinBox*>("targetRPM")->value(),2912);
        auto spin=w.findChild<QSpinBox*>("targetRPM");spin->setValue(2300);QCOMPARE(api.writes.size(),1);QTest::mouseClick(named(w,"应用转速"),Qt::LeftButton);QTRY_COMPARE(api.writes.size(),2);QCOMPARE(api.writes[1].second.value("slot").toInt(),3);QCOMPARE(api.writes[1].second.value("rpm").toInt(),2300);
    }
    void editingSlotLoadsPresetAndPreservesAscendingSpeeds(){
        Fixture api;MainWindow w(api.url());w.show();QTRY_VERIFY(preset(w,2)->isEnabled());
        auto target=w.findChild<QSpinBox*>("targetRPM");auto editSlot=w.findChild<QComboBox*>("editSlot");
        editSlot->setCurrentIndex(1);QTRY_COMPARE(target->value(),2134);QTRY_VERIFY(named(w,"应用转速")->isEnabled());
        target->setValue(1500);QTest::mouseClick(named(w,"应用转速"),Qt::LeftButton);QCOMPARE(api.writes.size(),0);
        target->setValue(2912);QTest::mouseClick(named(w,"应用转速"),Qt::LeftButton);QCOMPARE(api.writes.size(),0);
        target->setValue(2300);QTest::mouseClick(named(w,"应用转速"),Qt::LeftButton);QTRY_COMPARE(api.writes.size(),1);
        QCOMPARE(api.writes[0].second,QJsonObject({{"source",0},{"slot",2},{"rpm",2300}}));
    }
    void screenReadbackAndUnknownLayout(){
        Fixture api;MainWindow w(api.url());w.show();QTest::mouseClick(named(w,"屏幕"),Qt::LeftButton);QTRY_VERIFY(named(w,"开启")->isEnabled());QVERIFY(named(w,"开启")->isChecked());
        for(int i=0;i<3;i++)QCOMPARE(w.findChild<QComboBox*>(QString("lcdRow%1").arg(i))->currentIndex(),0);QVERIFY(!named(w,"应用布局")->isEnabled());
        for(int i=0;i<3;i++)w.findChild<QComboBox*>(QString("lcdRow%1").arg(i))->setCurrentIndex(1);QTest::mouseClick(named(w,"应用布局"),Qt::LeftButton);QCOMPARE(api.writes.size(),0);
        w.findChild<QComboBox*>("lcdRow1")->setCurrentIndex(5);w.findChild<QComboBox*>("lcdRow2")->setCurrentIndex(8);QTest::mouseClick(named(w,"应用布局"),Qt::LeftButton);QTRY_COMPARE(api.writes.size(),1);QCOMPARE(api.writes[0].first,"/api/lcd-layout");QCOMPARE(api.writes[0].second.value("rows").toArray(),QJsonArray({"cpu-temperature","fan-rpm","time"}));
    }
    void paletteUsesExplicitColorMode(){
        Fixture api;MainWindow w(api.url());w.show();QTest::mouseClick(named(w,"灯效"),Qt::LeftButton);auto mode=w.findChild<QComboBox*>("colorMode");QTRY_VERIFY(mode->isEnabled());QCOMPARE(mode->currentIndex(),0);mode->setCurrentIndex(1);QTest::mouseClick(named(w,"应用灯效"),Qt::LeftButton);QTRY_COMPARE(api.writes.size(),1);QCOMPARE(api.writes[0].second,QJsonObject({{"colorMode","single"}}));QTRY_VERIFY(mode->isEnabled());mode->setCurrentIndex(0);QTest::mouseClick(named(w,"应用灯效"),Qt::LeftButton);QTRY_COMPARE(api.writes.size(),2);QCOMPARE(api.writes[1].second,QJsonObject({{"colorMode","multi"}}));
    }
    void liveRefreshPreservesManualDraftAndTracksDevice(){
        Fixture api;MainWindow w(api.url());w.show();QTRY_VERIFY(preset(w,1)->isEnabled());auto rpm=w.findChild<QSpinBox*>("targetRPM");rpm->setValue(2100);api.cooling={{"source",0},{"slot",4},{"rpm",3534}};api.lcd=false;QTRY_VERIFY_WITH_TIMEOUT(preset(w,4)->isChecked(),8000);QCOMPARE(rpm->value(),2100);QTRY_VERIFY(named(w,"关闭")->isChecked());QCOMPARE(api.writes.size(),0);
    }
    void reconnectReloadsWithoutReplayingActions(){
        Fixture api;MainWindow w(api.url());w.show();QTRY_VERIFY(preset(w,1)->isEnabled());api.connected=false;QTRY_VERIFY(!preset(w,1)->isEnabled());api.connected=true;api.generation++;api.rgb.insert("colorCount",1);QTRY_VERIFY(preset(w,1)->isEnabled());QCOMPARE(w.findChild<QComboBox*>("colorMode")->currentIndex(),1);QCOMPARE(api.writes.size(),0);
        api.running=true;QTRY_VERIFY(!preset(w,1)->isEnabled());QVERIFY(w.findChild<QPushButton*>("closeWindow")->isEnabled());api.running=false;QTRY_VERIFY(preset(w,1)->isEnabled());
    }
    void failedDeviceReadCannotApplyDefaultOptions(){
        Fixture api;api.failDevice=true;MainWindow w(api.url());w.show();QTest::mouseClick(named(w,"设置"),Qt::LeftButton);QTRY_VERIFY(preset(w,1)->isEnabled());QVERIFY(!named(w,"保存设置")->isEnabled());api.failDevice=false;QTRY_VERIFY_WITH_TIMEOUT(named(w,"保存设置")->isEnabled(),8000);QVERIFY(w.findChild<QCheckBox*>("smartStartStop")->isChecked());QCOMPARE(api.writes.size(),0);
    }
    void overviewOutputsReadBackAndSwitchIndependently(){
        Fixture api;api.lcd=false;MainWindow w(api.url());w.show();auto lcd=w.findChild<QCheckBox*>("overviewLCD");auto rgb=w.findChild<QCheckBox*>("overviewRGB");QTRY_VERIFY(lcd->isEnabled()&&rgb->isEnabled());QVERIFY(!lcd->isChecked());QVERIFY(rgb->isChecked());QCOMPARE(api.writes.size(),0);
        lcd->click();QTRY_COMPARE(api.writes.size(),1);QCOMPARE(api.writes[0].second,QJsonObject({{"kind","lcd"},{"on",true}}));QTRY_VERIFY(lcd->isEnabled()&&lcd->isChecked());QVERIFY(named(w,"开启")->isChecked());
        rgb->click();QTRY_COMPARE(api.writes.size(),2);QCOMPARE(api.writes[1].second,QJsonObject({{"kind","rgb"},{"on",false}}));QTRY_VERIFY(rgb->isEnabled()&&!rgb->isChecked());QVERIFY(named(w,"关灯")->isChecked());QVERIFY(lcd->isChecked());QCOMPARE(api.cooling.value("rpm").toInt(),1500);
        api.failSwitch=true;rgb->click();QTRY_COMPARE(api.writes.size(),3);QTRY_VERIFY(rgb->isEnabled());QVERIFY(!rgb->isChecked());QVERIFY(w.findChild<QLabel*>("feedback")->text().contains("switch rejected"));
        api.running=true;QTRY_VERIFY(!lcd->isEnabled()&&!rgb->isEnabled());api.running=false;api.connected=false;QTRY_VERIFY(!lcd->isEnabled()&&!rgb->isEnabled());api.connected=true;api.generation++;api.lcd=false;api.rgbEnabled=true;QTRY_VERIFY(lcd->isEnabled()&&rgb->isEnabled());QVERIFY(!lcd->isChecked());QVERIFY(rgb->isChecked());QCOMPARE(api.writes.size(),3);
    }
    void unavailableRGBSwitchStateIsNotAssumedOff(){
        Fixture api;api.missingRGBState=true;MainWindow w(api.url());w.show();auto rgb=w.findChild<QCheckBox*>("overviewRGB");auto lcd=w.findChild<QCheckBox*>("overviewLCD");QTRY_VERIFY(lcd->isEnabled());QVERIFY(!rgb->isEnabled());QCOMPARE(rgb->text(),"未读取");QVERIFY(!named(w,"关灯")->isEnabled());QCOMPARE(api.writes.size(),0);api.missingRGBState=false;QTRY_VERIFY_WITH_TIMEOUT(rgb->isEnabled()&&rgb->isChecked(),8000);QCOMPARE(api.writes.size(),0);
    }
    void tokenRefreshAfterBackendRestart(){
        Fixture server;ApiClient api(server.url());int callbacks=0;api.post("/api/query",{{"kind","lcd-switch"}},[&](ApiResult r){QVERIFY(r.ok());callbacks++;});QTRY_COMPARE(callbacks,1);server.token="ddeeff";api.post("/api/query",{{"kind","lcd-switch"}},[&](ApiResult r){QVERIFY(r.ok());callbacks++;});QTRY_COMPARE(callbacks,2);QCOMPARE(server.rootReads,2);
    }
};
QTEST_MAIN(NativeWindowTests)
#include "tests.moc"
