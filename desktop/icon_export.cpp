#include <QGuiApplication>
#include <QIcon>
#include <QBuffer>
#include <QFile>
#include <QDataStream>
int main(int argc,char **argv){QGuiApplication app(argc,argv);if(argc!=2)return 1;QIcon icon(":/icon.svg");QList<QByteArray> frames;for(int size:{16,24,32,48,64,128,256}){QByteArray png;QBuffer buffer(&png);buffer.open(QIODevice::WriteOnly);if(!icon.pixmap(size,size).save(&buffer,"PNG"))return 1;frames.append(png);}QFile file(argv[1]);if(!file.open(QIODevice::WriteOnly))return 1;QDataStream out(&file);out.setByteOrder(QDataStream::LittleEndian);out<<quint16(0)<<quint16(1)<<quint16(frames.size());quint32 offset=6+16*frames.size();int i=0;for(int size:{16,24,32,48,64,128,256}){out<<quint8(size==256?0:size)<<quint8(size==256?0:size)<<quint8(0)<<quint8(0)<<quint16(1)<<quint16(32)<<quint32(frames[i].size())<<offset;offset+=frames[i++].size();}for(const auto &png:frames)out.writeRawData(png.constData(),png.size());return out.status()==QDataStream::Ok?0:1;}
