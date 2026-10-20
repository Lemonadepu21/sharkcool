#include "window.h"
#include <QApplication>
#include <QCommandLineParser>
#include <QFile>
#include <QIcon>
#include <QLocalServer>
#include <QLocalSocket>
#include <QLockFile>
#include <QStandardPaths>
#include <QDir>
#include <QHostAddress>
#include <QCryptographicHash>
#include <cstdio>

int main(int argc,char **argv){
    // This runs without a GUI connection, allowing installers to verify runtime dependencies.
    for(int i=1;i<argc;i++)if(QString::fromLocal8Bit(argv[i])=="--version"){std::puts("SharkCool Desktop 1.14.0");return 0;}
    QApplication app(argc,argv);app.setApplicationName("sharkcool");app.setOrganizationName("SharkCool");app.setApplicationVersion("1.14.0");app.setDesktopFileName("sharkcool");app.setWindowIcon(QIcon(":/icon.svg"));app.setStyle("Fusion");
    QCommandLineParser parser;parser.addHelpOption();parser.addOption({"url","本机驱动地址","url","http://127.0.0.1:17865"});parser.addOption({"tray","启动后驻留托盘"});parser.addOption({"preview-page","预览页面 0..3","page","0"});parser.addOption({"render-preview","导出原生界面预览后退出","path"});parser.process(app);
    QUrl url(parser.value("url"));if(url.scheme()!="http"||!QHostAddress(url.host()).isLoopback()||url.port()<=0||!url.userInfo().isEmpty()||!url.query().isEmpty()||!url.fragment().isEmpty()||(!url.path().isEmpty()&&url.path()!="/")){std::fputs("只允许本机回环驱动地址\n",stderr);return 1;}
    url.setPath("");url.setHost(QHostAddress(url.host()).toString());
    QFile style(":/style.qss");if(style.open(QIODevice::ReadOnly))app.setStyleSheet(QString::fromUtf8(style.readAll()));
    QPalette palette;palette.setColor(QPalette::Window,QColor("#111419"));palette.setColor(QPalette::Base,QColor("#191d23"));palette.setColor(QPalette::Text,QColor("#e4e8ef"));palette.setColor(QPalette::WindowText,QColor("#e4e8ef"));palette.setColor(QPalette::ButtonText,QColor("#e4e8ef"));palette.setColor(QPalette::Highlight,QColor("#365447"));app.setPalette(palette);
    const bool preview=parser.isSet("render-preview");
    const QString name="sharkcool-window-"+QString::fromLatin1(QCryptographicHash::hash(url.toEncoded(),QCryptographicHash::Sha256).toHex().left(16));
    auto cache=QStandardPaths::writableLocation(QStandardPaths::CacheLocation);QDir().mkpath(cache);QLockFile lock(cache+"/"+name+".lock");QLocalServer server;
    if(!preview){
        QLocalSocket existing;existing.connectToServer(name);if(existing.waitForConnected(400)){existing.write("activate\n");existing.waitForBytesWritten(400);return 0;}
        if(!lock.tryLock(500)){existing.abort();existing.connectToServer(name);if(existing.waitForConnected(1000)){existing.write("activate\n");existing.waitForBytesWritten(400);return 0;}std::fputs("已有应用实例正在启动\n",stderr);return 1;}
        QLocalServer::removeServer(name);server.setSocketOptions(QLocalServer::UserAccessOption);if(!server.listen(name)){std::fputs("无法建立应用实例通信\n",stderr);return 1;}
    }
    MainWindow window(url);QObject::connect(&server,&QLocalServer::newConnection,&window,[&]{while(server.hasPendingConnections()){auto socket=server.nextPendingConnection();QObject::connect(socket,&QLocalSocket::readyRead,&window,[&,socket]{if(socket->readAll().contains("activate"))window.activate();socket->disconnectFromServer();});QObject::connect(socket,&QLocalSocket::disconnected,socket,&QObject::deleteLater);}});std::fprintf(stderr,"SharkCool Desktop 1.14.0; platform=%s; app-id=%s; frameless=%s\n",qPrintable(QGuiApplication::platformName()),qPrintable(app.desktopFileName()),window.windowFlags().testFlag(Qt::FramelessWindowHint)?"true":"false");
    if(!preview)window.enableTray(parser.isSet("tray"));
    if(preview){window.show();window.previewPage(parser.value("preview-page").toInt());const auto path=parser.value("render-preview");QTimer::singleShot(3500,&window,[&window,path,&app]{app.exit(window.grab().save(path)?0:1);});}
    return app.exec();
}
