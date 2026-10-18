#include "window.h"
#include <QtWidgets>
#include <QWindow>
#include <QDateTime>
#include <QJsonDocument>
#include <QPainter>
#include <algorithm>
#include <cmath>

namespace {
QLabel *label(const QString &text, const char *name="", QWidget *parent=nullptr) {
    auto p=new QLabel(text,parent); p->setObjectName(name); p->setTextFormat(Qt::PlainText); return p;
}
QPushButton *button(const QString &text, const char *name="", QWidget *parent=nullptr) {
    auto p=new QPushButton(text,parent);p->setObjectName(name);p->setCursor(Qt::PointingHandCursor);return p;
}
QFrame *card(QVBoxLayout **layout) {
    auto p=new QFrame; p->setObjectName("card");*layout=new QVBoxLayout(p);(*layout)->setContentsMargins(24,22,24,22);(*layout)->setSpacing(16);return p;
}
QWidget *scrollPage(QWidget *contents) {
    auto scroll=new QScrollArea;scroll->setWidgetResizable(true);scroll->setFrameShape(QFrame::NoFrame);scroll->setWidget(contents);return scroll;
}
void title(QVBoxLayout *layout, const QString &name, QWidget *extra=nullptr) {
    auto row=new QHBoxLayout;row->addWidget(label(name,"sectionTitle"));row->addStretch();if(extra)row->addWidget(extra);layout->addLayout(row);
}
QComboBox *combo(const char *name, const QStringList &items={}) {
    auto p=new QComboBox;p->setObjectName(name);p->addItems(items);p->setMinimumHeight(36);return p;
}
QSpinBox *spin(const char *name, int min, int max, int value, const QString &suffix={}) {
    auto p=new QSpinBox;p->setObjectName(name);p->setRange(min,max);p->setValue(value);p->setSuffix(suffix);p->setMinimumHeight(36);p->setButtonSymbols(QAbstractSpinBox::PlusMinus);return p;
}
QSlider *slider(const char *name, int min=0,int max=100) {
    auto p=new QSlider(Qt::Horizontal);p->setObjectName(name);p->setRange(min,max);p->setMinimumHeight(28);return p;
}
void field(QGridLayout *grid, int row, int column, const QString &name, QWidget *widget) {
    grid->addWidget(label(name,"fieldLabel"),row,column);grid->addWidget(widget,row+1,column);
}
QString description(const QJsonObject &c) {
    return c.contains("points")?QString("温控曲线 · %1").arg(c.value("source").toInt()==0?"CPU":"GPU"):QString::number(c.value("rpm").toInt())+" RPM";
}
const QStringList effectIDs={"flow","cycle","breathing","static","blinking","reactive","audio","refresh"};
const QStringList effectNames={"彩色流动","颜色循环","呼吸","常亮","闪烁","响应","音频同步","刷新"};
const QStringList metricIDs={"cpu-temperature","gpu-temperature","cpu-usage","gpu-usage","fan-rpm","disk-usage","memory-usage","time"};
const QStringList metricNames={"CPU 温度","GPU 温度","CPU 利用率","GPU 利用率","风扇转速","磁盘使用率","内存使用率","时钟"};
QPair<int,int> bounds(int effect) {
    switch(effect){case 1:return {1500,4000};case 2:return {5000,15000};case 3:return {1000,5000};case 5:return {450,1050};case 6:return {300,1000};case 8:return {250,750};default:return {0,0};}
}
class DragBar : public QFrame {
protected:
    void mousePressEvent(QMouseEvent *e) override { if(e->button()==Qt::LeftButton&&window()->windowHandle())window()->windowHandle()->startSystemMove(); }
    void mouseDoubleClickEvent(QMouseEvent *e) override { if(e->button()==Qt::LeftButton){if(window()->isMaximized())window()->showNormal();else window()->showMaximized();} }
};
}

class Gauge : public QWidget {
    int rpm=0;bool valid=false;
public:
    Gauge(){setMinimumSize(160,160);setSizePolicy(QSizePolicy::Expanding,QSizePolicy::Expanding);setObjectName("rpmGauge");}
    void sample(int value,bool ok){rpm=value;valid=ok;setAccessibleName(ok?QString("实时转速 %1 RPM").arg(value):"实时转速不可用");update();}
protected:
    void paintEvent(QPaintEvent*) override {
        QPainter p(this);p.setRenderHint(QPainter::Antialiasing);
        const qreal side=qMin(width(),height())-24;QRectF ring((width()-side)/2,(height()-side)/2,side,side);
        p.setPen(QPen(QColor("#30343d"),5,Qt::SolidLine,Qt::RoundCap));p.drawArc(ring,225*16,-270*16);
        if(valid){p.setPen(QPen(QColor("#b2e6d0"),5,Qt::SolidLine,Qt::RoundCap));p.drawArc(ring,225*16,-qBound(0.,rpm/4000.,1.)*270*16);}
        p.setPen(QColor("#626b78"));const auto center=ring.center();
        for(int i=0;i<=36;i++){const double a=(135.+i*270./36.)*std::acos(-1.)/180.;const qreal r=side/2-12;p.drawLine(QPointF(center.x()+cos(a)*r,center.y()+sin(a)*r),QPointF(center.x()+cos(a)*(r-4),center.y()+sin(a)*(r-4)));}
        auto font=p.font();font.setPixelSize(42);font.setWeight(QFont::DemiBold);p.setFont(font);p.setPen(QColor("#f2f4f7"));p.drawText(ring.adjusted(0,20,0,-20),Qt::AlignCenter,valid?QString::number(rpm):"—");
        font.setPixelSize(11);font.setWeight(QFont::Normal);p.setFont(font);p.setPen(QColor("#8c95a4"));p.drawText(QRectF(0,center.y()+25,width(),22),Qt::AlignCenter,"RPM");
    }
};
class MetricCard : public QWidget {
    QString name;QString value="—";QVector<double> samples;QColor accent;
public:
    MetricCard(QString title,QColor color):name(std::move(title)),accent(color){setMinimumSize(160,180);setObjectName("metricCard");}
    void sample(QJsonValue v){if(v.isDouble()){value=QString::number(v.toInt());samples.append(v.toDouble());if(samples.size()>36)samples.removeFirst();}else{value="—";samples.clear();}setAccessibleName(name+" "+value+" °C");update();}
protected:
    void paintEvent(QPaintEvent*) override {
        QPainter p(this);p.setRenderHint(QPainter::Antialiasing);p.setPen(QPen(QColor("#2a2e36"),1));p.setBrush(QColor("#191d23"));p.drawRoundedRect(rect().adjusted(1,1,-1,-1),16,16);
        auto f=p.font();f.setPixelSize(12);p.setFont(f);p.setPen(QColor("#949cab"));p.drawText(22,34,name+" 温度");
        f.setPixelSize(42);f.setWeight(QFont::DemiBold);p.setFont(f);p.setPen(QColor("#eff2f6"));p.drawText(22,94,value);
        const int unitX=22+p.fontMetrics().horizontalAdvance(value)+8;f.setPixelSize(12);f.setWeight(QFont::Normal);p.setFont(f);p.setPen(QColor("#949cab"));p.drawText(unitX,93,"°C");
        const QRectF chart(22,height()-52,width()-44,28);p.setPen(QPen(QColor("#30353e"),1));p.drawLine(chart.bottomLeft(),chart.bottomRight());
        if(samples.size()>1){QPainterPath path;for(int i=0;i<samples.size();i++){QPointF v(chart.left()+i*chart.width()/qMax(1,samples.size()-1),chart.bottom()-qBound(0.,samples[i]/100.,1.)*chart.height());if(i==0)path.moveTo(v);else path.lineTo(v);}p.setPen(QPen(accent,1.7));p.drawPath(path);}
    }
};
class CurveChart : public QWidget {
public:
    QJsonArray points;
    CurveChart(){setMinimumHeight(100);setObjectName("curveChart");}
protected:
    void paintEvent(QPaintEvent*) override {
        QPainter p(this);p.setRenderHint(QPainter::Antialiasing);QRectF chart(10,12,width()-20,height()-24);
        p.setPen(QColor("#303640"));for(int i=0;i<5;i++)p.drawLine(QPointF(chart.left(),chart.top()+i*chart.height()/4),QPointF(chart.right(),chart.top()+i*chart.height()/4));
        QPainterPath path;QVector<QPointF> markers;
        for(auto item:points){auto v=item.toObject();QPointF pt(chart.left()+v.value("temperature").toInt()/100.*chart.width(),chart.bottom()-(v.value("rpm").toInt()-1200)/2800.*chart.height());markers.append(pt);if(markers.size()==1)path.moveTo(pt);else path.lineTo(pt);}
        p.setPen(QPen(QColor("#b2e6d0"),2));p.drawPath(path);p.setBrush(QColor("#b2e6d0"));for(auto pt:markers)p.drawEllipse(pt,3,3);
    }
};

MainWindow::MainWindow(const QUrl &base,QWidget *parent):QWidget(parent),api(base,this) {
    setObjectName("appRoot");setWindowTitle("SharkCool");setWindowIcon(QIcon(":/icon.svg"));setWindowFlags(Qt::Window|Qt::FramelessWindowHint);setMinimumSize(960,680);resize(1100,860);
    auto root=new QVBoxLayout(this);root->setContentsMargins(0,0,0,0);root->setSpacing(0);
    auto header=new DragBar;header->setObjectName("windowBar");auto chrome=new QHBoxLayout(header);chrome->setContentsMargins(18,9,12,9);
    auto logo=label("");logo->setPixmap(QIcon(":/icon.svg").pixmap(22,22));chrome->addWidget(logo);chrome->addWidget(label("SharkCool","windowBrand"));chrome->addStretch();
    for(const auto &text:QStringList{"−","□","×"}){auto b=button(text,text=="×"?"closeWindow":"windowButton");b->setFixedSize(30,28);chrome->addWidget(b);if(text=="×")connect(b,&QPushButton::clicked,this,&QWidget::close);else if(text=="−")connect(b,&QPushButton::clicked,this,&QWidget::showMinimized);else connect(b,&QPushButton::clicked,this,[this]{isMaximized()?showNormal():showMaximized();});}
    root->addWidget(header);
    auto body=new QHBoxLayout;body->setContentsMargins(0,0,0,0);body->setSpacing(0);root->addLayout(body,1);
    auto sidebar=new QFrame;sidebar->setObjectName("sidebar");sidebar->setFixedWidth(172);auto nav=new QVBoxLayout(sidebar);nav->setContentsMargins(16,28,16,20);nav->setSpacing(9);
    auto identity=new QVBoxLayout;identity->setSpacing(7);auto mark=label("");mark->setPixmap(QIcon(":/icon.svg").pixmap(42,42));identity->addWidget(mark);identity->addWidget(label("风神 Pro","productName"));identity->addWidget(label("BRB02 CONTROLLER","eyebrow"));nav->addLayout(identity);nav->addSpacing(28);
    pages=new QStackedWidget;pages->setObjectName("pages");
    auto group=new QButtonGroup(this);group->setExclusive(true);int index=0;
    for(const auto &text:QStringList{"概览","灯效","屏幕","设置"}){auto b=button(text,"navButton");b->setCheckable(true);b->setMinimumHeight(44);group->addButton(b,index);nav->addWidget(b);if(index==0)b->setChecked(true);connect(b,&QPushButton::clicked,this,[this,index]{pages->setCurrentIndex(index);pageHeading->setText(QStringList{"设备概览","灯光效果","屏幕设置","应用设置"}.value(index));});index++;}
    nav->addStretch();transport=label("等待连接","muted");nav->addWidget(transport);nav->addWidget(label("SharkCool 1.13.0","version"));body->addWidget(sidebar);
    auto main=new QVBoxLayout;main->setContentsMargins(30,22,30,22);main->setSpacing(18);body->addLayout(main,1);
    auto top=new QHBoxLayout;auto headings=new QVBoxLayout;headings->setSpacing(4);pageHeading=label("设备概览","pageTitle");headings->addWidget(pageHeading);headings->addWidget(label("BLACK SHARK · 风神 Pro","eyebrow"));top->addLayout(headings);top->addStretch();badge=label("正在连接","connectionBadge");top->addWidget(badge,0,Qt::AlignVCenter);main->addLayout(top);
    pages->addWidget(scrollPage(dashboard()));pages->addWidget(scrollPage(lighting()));pages->addWidget(scrollPage(screen()));pages->addWidget(scrollPage(settings()));main->addWidget(pages,1);
    feedback=label("","feedback");feedback->setWordWrap(true);feedback->hide();main->addWidget(feedback);
    connect(&pollTimer,&QTimer::timeout,this,[this]{poll();});pollTimer.start(1000);updateEnabled();setMouseTracking(true);installEventFilter(this);for(auto child:findChildren<QWidget*>()){child->setMouseTracking(true);child->installEventFilter(this);}QTimer::singleShot(0,this,[this]{poll();});
}
void MainWindow::previewPage(int index){pages->setCurrentIndex(qBound(0,index,3));pageHeading->setText(QStringList{"设备概览","灯光效果","屏幕设置","应用设置"}.value(index));int i=0;for(auto b:findChildren<QPushButton*>("navButton")){b->setChecked(i++==index);}}
void MainWindow::activate(){showNormal();raise();activateWindow();}
void MainWindow::enableTray(bool startHidden){
    if(tray)return;
    qApp->setQuitOnLastWindowClosed(false);
    tray=new QSystemTrayIcon(windowIcon(),this);tray->setObjectName("systemTray");tray->setToolTip("SharkCool");
    auto menu=new QMenu(this);auto open=menu->addAction("打开 SharkCool");open->setObjectName("trayOpen");connect(open,&QAction::triggered,this,&MainWindow::activate);
    trayAutostart=menu->addAction("登录后自动运行");trayAutostart->setObjectName("trayAutostart");trayAutostart->setCheckable(true);trayAutostart->setEnabled(false);
    connect(trayAutostart,&QAction::triggered,this,[this]{autostart->click();});
    menu->addSeparator();trayExit=menu->addAction("退出 SharkCool");trayExit->setObjectName("trayExit");
    connect(trayExit,&QAction::triggered,this,[this]{trayExit->setEnabled(false);api.post("/api/quit",{},[this](ApiResult r){if(r.ok()||r.status==0)qApp->quit();else{trayExit->setEnabled(true);activate();message(r.error,true);}});});
    tray->setContextMenu(menu);connect(tray,&QSystemTrayIcon::activated,this,[this](QSystemTrayIcon::ActivationReason reason){if(reason==QSystemTrayIcon::Trigger||reason==QSystemTrayIcon::DoubleClick)activate();});tray->show();
    if(startHidden&&QSystemTrayIcon::isSystemTrayAvailable())hide();else show();
}
void MainWindow::closeEvent(QCloseEvent *event){
    if(tray&&tray->isVisible()&&QSystemTrayIcon::isSystemTrayAvailable()){hide();event->ignore();return;}
    QWidget::closeEvent(event);if(tray)qApp->quit();
}
QWidget *MainWindow::dashboard(){
    auto page=new QWidget;auto layout=new QVBoxLayout(page);layout->setContentsMargins(0,0,0,0);layout->setSpacing(18);
    auto metrics=new QHBoxLayout;metrics->setSpacing(14);QVBoxLayout *rpmLayout;auto rpmCard=card(&rpmLayout);rpmCard->setMinimumWidth(290);rpmLayout->setContentsMargins(20,16,20,8);rpmLayout->setSpacing(0);rpmLayout->addWidget(label("实时转速","fieldLabel"));gauge=new Gauge;rpmLayout->addWidget(gauge);metrics->addWidget(rpmCard,2);cpuCard=new MetricCard("CPU",QColor("#b2e6d0"));gpuCard=new MetricCard("GPU",QColor("#a8b8ee"));metrics->addWidget(cpuCard,1);metrics->addWidget(gpuCard,1);layout->addLayout(metrics);
    auto outputs=new QHBoxLayout;outputs->setSpacing(14);
    for(const QString &kind:QStringList{"lcd","rgb"}){QVBoxLayout *content;auto tile=card(&content);tile->setMinimumHeight(76);content->setContentsMargins(22,12,22,12);auto row=new QHBoxLayout;row->addWidget(label(kind=="lcd"?"屏幕":"灯效","sectionTitle"));row->addStretch();auto toggle=new QCheckBox("读取中");toggle->setObjectName(kind=="lcd"?"overviewLCD":"overviewRGB");toggle->setProperty("outputSwitch",true);toggle->setCursor(Qt::PointingHandCursor);toggle->setAccessibleName(kind=="lcd"?"屏幕开关":"灯效开关");row->addWidget(toggle);content->addLayout(row);outputs->addWidget(tile,1);if(kind=="lcd")overviewLCD=toggle;else overviewRGB=toggle;connect(toggle,&QCheckBox::clicked,this,[this,kind](bool on){renderOutputs();setOutput(kind,on);});}layout->addLayout(outputs);
    QVBoxLayout *fan;auto panel=card(&fan);fan->setSpacing(10);current=label("读取当前配置…","currentCooling");title(fan,"风扇控制",current);
    auto presetRow=new QHBoxLayout;presetRow->setSpacing(10);for(int i=0;i<4;i++){auto b=button(QString("%1 档").arg(i+1),"slotButton");b->setProperty("slot",i+1);b->setCheckable(true);b->setMinimumHeight(62);b->setAccessibleName(QString("%1 档").arg(i+1));slotButtons[i]=b;presetRow->addWidget(b,1);connect(b,&QPushButton::clicked,this,[this,i]{renderPresets();applySlot(i);});}fan->addLayout(presetRow);
    auto controls=new QHBoxLayout;format=combo("coolingFormat",{"固定转速","温控曲线"});controls->addWidget(label("控制方式","fieldLabel"));controls->addWidget(format);controls->addStretch();fan->addLayout(controls);editors<<format;
    fixedEditor=new QWidget;auto fixed=new QVBoxLayout(fixedEditor);fixed->setContentsMargins(0,0,0,0);fixed->setSpacing(10);auto target=new QHBoxLayout;target->addWidget(label("目标转速","fieldLabel"));target->addStretch();rpm=spin("targetRPM",1200,4000,1800," RPM");rpm->setFixedWidth(160);target->addWidget(rpm);fixed->addLayout(target);rpmSlider=slider("rpmSlider",1200,4000);fixed->addWidget(rpmSlider);auto range=new QHBoxLayout;range->addWidget(label("1200","subtle"));range->addStretch();range->addWidget(label("4000 RPM","subtle"));fixed->addLayout(range);fan->addWidget(fixedEditor);editors<<rpm<<rpmSlider;
    connect(rpm,qOverload<int>(&QSpinBox::valueChanged),this,[this](int n){QSignalBlocker block(rpmSlider);rpmSlider->setValue(n);if(!rendering)coolingDirty=true;});connect(rpmSlider,&QSlider::valueChanged,this,[this](int n){rpm->setValue(n);});
    curveEditor=new QWidget;auto curve=new QVBoxLayout(curveEditor);curve->setContentsMargins(0,0,0,0);curveChart=new CurveChart;curve->addWidget(curveChart);auto points=new QGridLayout;points->setHorizontalSpacing(12);for(int i=0;i<4;i++){curveTemps[i]=spin(qPrintable(QString("curveTemp%1").arg(i)),0,100,20+i*20," °C");curveRPMs[i]=spin(qPrintable(QString("curveRPM%1").arg(i)),1200,4000,1200+i*600," RPM");points->addWidget(label(QString("点 %1").arg(i+1),"fieldLabel"),0,i);points->addWidget(curveTemps[i],1,i);points->addWidget(curveRPMs[i],2,i);editors<<curveTemps[i]<<curveRPMs[i];for(auto input:{curveTemps[i],curveRPMs[i]})connect(input,qOverload<int>(&QSpinBox::valueChanged),this,[this]{if(!rendering)coolingDirty=true;updateCurve();});}curve->addLayout(points);curve->addWidget(label("温控曲线需要开启温度同步","subtle"));fan->addWidget(curveEditor);curveEditor->hide();updateCurve();
    auto advanced=button("配置选项  ▾","textButton");advanced->setCheckable(true);fan->addWidget(advanced,0,Qt::AlignLeft);advancedEditor=new QWidget;auto advancedRow=new QHBoxLayout(advancedEditor);advancedRow->setContentsMargins(0,0,0,0);slot=combo("editSlot",{"1 档","2 档","3 档","4 档"});source=combo("coolingSource",{"CPU","GPU"});readPreset=button("载入配置","secondary");advancedRow->addWidget(label("编辑档位","fieldLabel"));advancedRow->addWidget(slot);advancedRow->addWidget(label("温度来源","fieldLabel"));advancedRow->addWidget(source);advancedRow->addStretch();advancedRow->addWidget(readPreset);fan->addWidget(advancedEditor);advancedEditor->hide();editors<<slot<<source;connect(advanced,&QPushButton::toggled,advancedEditor,&QWidget::setVisible);
    connect(format,qOverload<int>(&QComboBox::currentIndexChanged),this,[this]{coolingFields();if(!rendering){coolingDirty=true;invalidatePresets();syncDevice();}});for(auto c:{slot,source})connect(c,qOverload<int>(&QComboBox::currentIndexChanged),this,[this]{if(!rendering)coolingDirty=true;});
    connect(readPreset,&QPushButton::clicked,this,[this]{const QString key=connectionKey;busy=true;updateEnabled();api.post("/api/query",{{"kind","cooling-preset"},{"curve",format->currentIndex()==1},{"slot",slot->currentIndex()+1}},[this,key](ApiResult r){busy=false;if(key==connectionKey&&r.ok()&&r.data.value("config").isObject()){showCooling(r.data.value("config").toObject(),false);coolingDirty=true;message("配置已载入，点击应用后生效");}else message(r.error.isEmpty()?"连接已变化，请重试":r.error,true);updateEnabled();});});
    connect(slot,qOverload<int>(&QComboBox::currentIndexChanged),this,[this]{if(!rendering)readPreset->click();});
    auto actions=new QHBoxLayout;actions->addStretch();applyCooling=button("应用转速","primary");applyCooling->setAccessibleName("应用转速");actions->addWidget(applyCooling);fan->addLayout(actions);connect(applyCooling,&QPushButton::clicked,this,[this]{applyManualCooling();});layout->addWidget(panel);layout->addStretch();return page;
}
QWidget *MainWindow::lighting(){
    auto page=new QWidget;auto layout=new QVBoxLayout(page);layout->setContentsMargins(0,0,0,0);layout->setSpacing(18);
    QVBoxLayout *effects;auto effectCard=card(&effects);auto switchRow=new QWidget;auto sr=new QHBoxLayout(switchRow);sr->setContentsMargins(0,0,0,0);for(bool on:{true,false}){auto b=button(on?"开灯":"关灯","secondary");b->setCheckable(true);if(on)rgbOn=b;else rgbOff=b;sr->addWidget(b);connect(b,&QPushButton::clicked,this,[this,on]{renderOutputs();setOutput("rgb",on);});}title(effects,"灯光效果",switchRow);
    auto grid=new QGridLayout;grid->setSpacing(12);int cell=0;
    for(int i:{0,1,2,3,4,7}){auto b=button(effectNames[i],"effectButton");b->setCheckable(true);b->setMinimumHeight(88);QPixmap icon(100,24);icon.fill(Qt::transparent);QPainter painter(&icon);painter.setRenderHint(QPainter::Antialiasing);for(int j=0;j<7;j++){QColor color=QColor::fromHsv((i*25+j*42)%360,90+i*15,230);painter.setBrush(color);painter.setPen(Qt::NoPen);painter.drawRoundedRect(QRectF(j*14,6,9,i==3?12:8+(j+i)%4),3,3);}b->setIcon(QIcon(icon));b->setIconSize(QSize(100,24));effectButtons[i]=b;grid->addWidget(b,cell/3,cell%3);cell++;editors<<b;connect(b,&QPushButton::clicked,this,[this,i]{for(auto effect:effectButtons)if(effect)effect->setChecked(effect==effectButtons[i]);rgbDirty.insert("effect");if(!rgbDirty.contains("speed")){QSignalBlocker block(speed);speed->setValue(50);}lightingFields();});}effects->addLayout(grid);layout->addWidget(effectCard);
    QVBoxLayout *options;auto panel=card(&options);title(options,"色彩与动态");auto colors=new QHBoxLayout;colorMode=combo("colorMode",{"彩色","单色"});colorMode->setMinimumWidth(150);colorButton=button("选择颜色","colorButton");colorButton->setMinimumWidth(150);colors->addWidget(label("颜色模式","fieldLabel"));colors->addWidget(colorMode);colors->addStretch();colors->addWidget(colorButton);options->addLayout(colors);editors<<colorMode<<colorButton;
    auto bright=new QHBoxLayout;bright->addWidget(label("亮度","fieldLabel"));bright->addStretch();brightnessValue=label("80%","valueLabel");bright->addWidget(brightnessValue);options->addLayout(bright);brightness=slider("brightness");brightness->setValue(80);options->addWidget(brightness);editors<<brightness;
    speedField=new QWidget;auto dynamic=new QVBoxLayout(speedField);dynamic->setContentsMargins(0,0,0,0);auto speedRow=new QHBoxLayout;speedRow->addWidget(label("速度","fieldLabel"));speedRow->addStretch();speedValue=label("50%","valueLabel");speedRow->addWidget(speedValue);dynamic->addLayout(speedRow);speed=slider("lightingSpeed");speed->setValue(50);dynamic->addWidget(speed);options->addWidget(speedField);editors<<speed;
    connect(colorMode,qOverload<int>(&QComboBox::currentIndexChanged),this,[this]{if(!rendering){rgbDirty.insert("colorMode");if(colorMode->currentIndex()==0)rgbDirty.remove("color");}lightingFields();});
    connect(colorButton,&QPushButton::clicked,this,[this]{auto color=QColorDialog::getColor(selectedColor,this,"选择灯光颜色");if(color.isValid()){selectedColor=color;rgbDirty.insert("color");lightingFields();}});
    connect(brightness,&QSlider::valueChanged,this,[this](int n){brightnessValue->setText(QString::number(n)+"%");if(!rendering)rgbDirty.insert("brightness");});connect(speed,&QSlider::valueChanged,this,[this](int n){speedValue->setText(QString::number(n)+"%");if(!rendering)rgbDirty.insert("speed");});
    auto actions=new QHBoxLayout;actions->addStretch();applyRGB=button("应用灯效","primary");actions->addWidget(applyRGB);options->addLayout(actions);connect(applyRGB,&QPushButton::clicked,this,[this]{setLighting();});layout->addWidget(panel);layout->addStretch();return page;
}
QWidget *MainWindow::screen(){
    auto page=new QWidget;auto layout=new QVBoxLayout(page);layout->setContentsMargins(0,0,0,0);layout->setSpacing(18);
    QVBoxLayout *lcd;auto panel=card(&lcd);auto state=new QWidget;auto switches=new QHBoxLayout(state);switches->setContentsMargins(0,0,0,0);lcdOn=button("开启","secondary");lcdOff=button("关闭","secondary");for(auto b:{lcdOn,lcdOff}){b->setCheckable(true);switches->addWidget(b);connect(b,&QPushButton::clicked,this,[this,b]{const bool on=b==lcdOn;renderOutputs();setOutput("lcd",on);});}title(lcd,"屏幕布局",state);
    auto fields=new QGridLayout;fields->setSpacing(12);for(int i=0;i<3;i++){rows[i]=combo(qPrintable(QString("lcdRow%1").arg(i)));rows[i]->addItem("选择显示内容","");for(int j=0;j<metricIDs.size();j++)rows[i]->addItem(metricNames[j],metricIDs[j]);field(fields,0,i,QString("第%1行").arg(i+1),rows[i]);editors<<rows[i];connect(rows[i],qOverload<int>(&QComboBox::currentIndexChanged),this,[this]{updateEnabled();});}lcd->addLayout(fields);auto layoutActions=new QHBoxLayout;layoutState=label("当前布局无法从设备读取","subtle");layoutActions->addWidget(layoutState);layoutActions->addStretch();applyLayout=button("应用布局","primary");layoutActions->addWidget(applyLayout);lcd->addLayout(layoutActions);connect(applyLayout,&QPushButton::clicked,this,[this]{QJsonArray selected;QSet<QString> unique;for(auto row:rows){selected.append(row->currentData().toString());unique.insert(row->currentData().toString());}if(unique.size()!=3){message("请选择三个不同指标",true);return;}mutation("/api/lcd-layout",{{"rows",selected}},[this](QJsonObject){layoutState->setText("已发送布局，请核对屏幕显示");});});layout->addWidget(panel);
    QVBoxLayout *image;auto screenCard=card(&image);title(image,"屏保",label("428 × 142","subtle"));preview=label("选择一张图片","imagePreview");preview->setAlignment(Qt::AlignCenter);preview->setMinimumHeight(190);image->addWidget(preview);auto actions=new QHBoxLayout;chooseImage=button("选择图片","secondary");restoreImage=button("恢复原图","secondary");applyImage=button("应用屏保","primary");actions->addWidget(chooseImage);actions->addWidget(restoreImage);actions->addStretch();actions->addWidget(applyImage);image->addLayout(actions);progress=new QProgressBar;progress->setObjectName("screenProgress");progress->setTextVisible(false);progress->setRange(0,100);progress->hide();image->addWidget(progress);screenState=label("","subtle");screenState->setWordWrap(true);image->addWidget(screenState);connect(chooseImage,&QPushButton::clicked,this,[this]{prepareImage();});connect(applyImage,&QPushButton::clicked,this,[this]{mutation("/api/screensaver/apply",{{"draftId",draftID}},[this](QJsonObject s){screenBusy=s.value("running").toBool();updateEnabled();});});connect(restoreImage,&QPushButton::clicked,this,[this]{mutation("/api/screensaver/restore",{},[this](QJsonObject s){screenBusy=s.value("running").toBool();updateEnabled();});});layout->addWidget(screenCard);layout->addStretch();return page;
}
QWidget *MainWindow::settings(){
    auto page=new QWidget;auto layout=new QVBoxLayout(page);layout->setContentsMargins(0,0,0,0);layout->setSpacing(18);
    QVBoxLayout *device;auto panel=card(&device);title(device,"设备选项");smart=new QCheckBox("智能启停");smart->setObjectName("smartStartStop");powerOn=new QCheckBox("上电自启动");powerOn->setObjectName("powerOnStart");device->addWidget(smart);device->addWidget(powerOn);for(auto toggle:{smart,powerOn})connect(toggle,&QCheckBox::toggled,this,[this]{if(!rendering)deviceDirty=true;});auto actions=new QHBoxLayout;actions->addStretch();saveDevice=button("保存设置","primary");actions->addWidget(saveDevice);device->addLayout(actions);connect(saveDevice,&QPushButton::clicked,this,[this]{mutation("/api/device-options",{{"smartStartStop",smart->isChecked()},{"powerOnStart",powerOn->isChecked()}},[this](QJsonObject data){deviceDirty=false;applyReadback("device",data);});});layout->addWidget(panel);
    QVBoxLayout *application;auto prefs=card(&application);title(application,"应用设置");syncInfo=new QCheckBox("同步本机温度与系统信息");syncInfo->setObjectName("syncSystemInfo");autostart=new QCheckBox("登录后自动运行");autostart->setObjectName("loginAutostart");application->addWidget(syncInfo);application->addWidget(autostart);connect(syncInfo,&QCheckBox::toggled,this,[this](bool on){if(!rendering)mutation("/api/system-info",{{"enabled",on}});});connect(autostart,&QCheckBox::toggled,this,[this](bool on){if(!rendering)mutation("/api/autostart",{{"enabled",on}});});layout->addWidget(prefs);
    QVBoxLayout *info;auto about=card(&info);title(info,"SharkCool",label("1.13.0","subtle"));connectionDetail=label("正在读取设备连接…","subtle");connectionDetail->setWordWrap(true);info->addWidget(connectionDetail);layout->addWidget(about);layout->addStretch();return page;
}

void MainWindow::message(const QString &text,bool error){feedback->setText(text);feedback->setProperty("error",error);feedback->style()->unpolish(feedback);feedback->style()->polish(feedback);feedback->setVisible(!text.isEmpty());}
void MainWindow::resetConnection(QString key){
    connectionKey=std::move(key);revision++;coolingLoaded=rgbLoaded=deviceLoaded=lcdLoaded=rgbSwitchLoaded=presetsLoaded=false;coolingDirty=deviceDirty=false;rgbDirty.clear();currentCooling={};for(auto &preset:presets)preset={};nextSync=0;
    current->setText(connected?"正在读取…":"等待设备连接");renderOutputs();for(auto row:rows){QSignalBlocker block(row);row->setCurrentIndex(0);}layoutState->setText("当前布局无法从设备读取");renderPresets();
}
void MainWindow::poll(){
    if(polling)return;polling=true;
    api.get("/api/status",[this](ApiResult r){
        polling=false;
        if(!r.ok()){connected=false;if(!connectionKey.isEmpty())resetConnection("");badge->setText("服务未连接");transport->setText("等待服务");gauge->sample(0,false);cpuCard->sample({});gpuCard->sample({});updateEnabled();return;}
        const auto s=r.data;connected=s.value("connected").toBool();demo=s.value("demo").toBool();
        const QString key=connected&&!demo?s.value("session").toString()+"|"+s.value("device").toString()+"|"+s.value("transport").toString()+"|"+QString::number(s.value("connectionGeneration").toInt()):"";
        if(key!=connectionKey)resetConnection(key);
        const bool live=connected&&!s.value("stale").toBool();badge->setText(demo?"演示模式":live?"已连接":connected?"等待心跳":"设备未连接");badge->setProperty("online",live&&!demo);badge->style()->unpolish(badge);badge->style()->polish(badge);
        transport->setText(demo?"演示模式":connected?(s.value("transport").toString()=="ble"?"蓝牙连接":"USB 连接"):"设备离线");connectionDetail->setText(demo?"演示模式，不访问散热器":transport->text()+"\n"+s.value("device").toString()+"\n驱动 "+s.value("version").toString("旧版本服务，请重新启动"));gauge->sample(s.value("rpm").toInt(),live);const auto temps=s.value("temps").toObject();cpuCard->sample(temps.value("cpu"));gpuCard->sample(temps.value("gpu"));
        if(demo){current->setText("演示模式");renderPresets();}rpm->setMaximum(demo?3600:4000);rpmSlider->setMaximum(demo?3600:4000);updateEnabled();syncDevice();
    });
    if(!screenPolling){screenPolling=true;api.get("/api/screensaver",[this](ApiResult r){screenPolling=false;if(!r.ok()){screenState->setText("屏保状态未读取");return;}auto s=r.data;screenBusy=s.value("running").toBool();canRestore=s.value("canRestore").toBool();if(!draftID.isEmpty()&&draftID!=s.value("draftId").toString()){draftID.clear();preview->setPixmap({});preview->setText("选择一张图片");}progress->setVisible(screenBusy);const int n=s.value("progress").toInt();progress->setRange(0,n<0?0:100);if(n>=0)progress->setValue(n);screenState->setText(s.value("error").toString().isEmpty()?s.value("phase").toString():s.value("error").toString());updateEnabled();});}
    if((statusSequence++%5==0)&&!auxiliaryPolling){auxiliaryPolling=true;api.get("/api/settings",[this](ApiResult r){settingsLoaded=r.ok();if(r.ok()&&!busy){QSignalBlocker b(autostart);autostart->setChecked(r.data.value("autostart").toBool());}updateEnabled();api.get("/api/system-info",[this](ApiResult s){auxiliaryPolling=false;if(s.ok()&&!busy){QSignalBlocker b(syncInfo);syncInfo->setChecked(s.data.value("enabled").toBool());}});});}
}
void MainWindow::syncDevice(){if(syncing||busy||screenBusy||!connected||demo||connectionKey.isEmpty()||QDateTime::currentMSecsSinceEpoch()<nextSync)return;syncing=true;nextSync=QDateTime::currentMSecsSinceEpoch()+5000;syncKind(0,connectionKey,revision);}
void MainWindow::syncKind(int index,QString key,int epoch){
    if(key!=connectionKey||epoch!=revision||!connected||busy||screenBusy){syncing=false;return;}
    const QStringList kinds={"cooling","rgb","device","lcd","rgb-switch"};
    if(index>=kinds.size()){if(!presetsLoaded)loadPresets(0,key,format->currentIndex()==1,epoch);else{syncing=false;updateEnabled();}return;}
    const QString kind=kinds[index];if((kind=="rgb"&&rgbLoaded)||(kind=="device"&&deviceLoaded)){syncKind(index+1,key,epoch);return;}
    api.post("/api/query",{{"kind",kind=="lcd"?"lcd-switch":kind}},[this,index,key,epoch,kind](ApiResult r){
        if(key!=connectionKey||epoch!=revision||busy||screenBusy){syncing=false;return;}
        if(r.ok()&&((kind!="lcd"&&kind!="rgb-switch")||r.data.value("enabled").isBool()))applyReadback(kind,r.data);else if(kind=="lcd"||kind=="rgb-switch"){if(kind=="lcd")lcdLoaded=false;else rgbSwitchLoaded=false;renderOutputs();}else if(kind=="cooling")current->setText("当前配置未读取");
        syncKind(index+1,key,epoch);
    });
}
void MainWindow::loadPresets(int index,QString key,bool curve,int epoch){
    if(key!=connectionKey||epoch!=revision||busy||screenBusy||curve!=(format->currentIndex()==1)){syncing=false;return;}
    if(index==4){presetsLoaded=true;syncing=false;renderPresets();updateEnabled();return;}
    api.post("/api/query",{{"kind","cooling-preset"},{"curve",curve},{"slot",index+1}},[this,index,key,curve,epoch](ApiResult r){
        if(key!=connectionKey||epoch!=revision||busy||screenBusy||curve!=(format->currentIndex()==1)){syncing=false;return;}
        const auto c=r.data.value("config").toObject();if(!r.ok()||c.value("slot").toInt()!=index+1||c.contains("points")!=curve){syncing=false;presetsLoaded=false;updateEnabled();return;}
        presets[index]=c;renderPresets();loadPresets(index+1,key,curve,epoch);
    });
}
void MainWindow::invalidatePresets(){presetsLoaded=false;for(auto &p:presets)p={};revision++;nextSync=0;renderPresets();updateEnabled();}
void MainWindow::renderPresets(){
    const int demoRPMs[]={1200,2134,2912,3534};
    for(int i=0;i<4;i++){auto b=slotButtons[i];b->setText(QString("%1 档\n%2").arg(i+1).arg(demo?QString::number(demoRPMs[i])+" RPM":presets[i].isEmpty()?"读取中":description(presets[i])));b->setChecked(!currentCooling.isEmpty()&&currentCooling.value("slot").toInt()==i+1&&currentCooling.contains("points")==bool(format->currentIndex()==1));}
}
void MainWindow::applyReadback(const QString &kind,const QJsonObject &data){
    if(kind=="cooling"&&data.value("config").isObject()){coolingLoaded=true;showCooling(data.value("config").toObject(),true);}
    else if(kind=="lcd"&&data.value("enabled").isBool()){lcdLoaded=true;lcdEnabled=data.value("enabled").toBool();renderOutputs();}
    else if(kind=="rgb-switch"&&data.value("enabled").isBool()){rgbSwitchLoaded=true;rgbEnabled=data.value("enabled").toBool();renderOutputs();}
    else if(kind=="device"&&data.value("options").isObject()){deviceLoaded=true;if(!deviceDirty){const auto o=data.value("options").toObject();QSignalBlocker b1(smart),b2(powerOn);smart->setChecked(o.value("smartStartStop").toBool());powerOn->setChecked(o.value("powerOnStart").toBool());}}
    else if(kind=="rgb"&&data.value("config").isObject()){const auto c=data.value("config").toObject();const int effect=c.value("effect").toInt();const int count=c.value("colorCount").toInt();if(effect<1||effect>8||(count!=1&&count!=10))return;rgbLoaded=true;if(rgbDirty.isEmpty()){rendering=true;for(int i=0;i<8;i++)if(effectButtons[i])effectButtons[i]->setChecked(i==effect-1);colorMode->setCurrentIndex(count==10?0:1);auto rgb=c.value("color").toArray();if(rgb.size()==3)selectedColor=QColor(rgb[0].toInt(),rgb[1].toInt(),rgb[2].toInt());brightness->setValue(c.value("brightness").toInt());const auto limit=bounds(effect);if(limit.second>limit.first)speed->setValue(qBound(0,qRound(100.*(limit.second-c.value("interval").toInt())/(limit.second-limit.first)),100));rendering=false;lightingFields();}}
    updateEnabled();
}
void MainWindow::showCooling(const QJsonObject &c,bool active){
    if(active){currentCooling=c;current->setText(QString("%1 档 · %2").arg(c.value("slot").toInt()).arg(description(c)));}
    if(!active||!coolingDirty){rendering=true;const int newFormat=c.contains("points")?1:0;if(newFormat!=format->currentIndex()){presetsLoaded=false;for(auto &p:presets)p={};}format->setCurrentIndex(newFormat);slot->setCurrentIndex(c.value("slot").toInt()-1);source->setCurrentIndex(c.value("source").toInt());if(c.contains("points")){const auto points=c.value("points").toArray();for(int i=0;i<qMin(4,points.size());i++){curveTemps[i]->setValue(points[i].toObject().value("temperature").toInt());curveRPMs[i]->setValue(points[i].toObject().value("rpm").toInt());}}else rpm->setValue(c.value("rpm").toInt());rendering=false;coolingFields();updateCurve();}renderPresets();
}
void MainWindow::coolingFields(){fixedEditor->setVisible(format->currentIndex()==0);curveEditor->setVisible(format->currentIndex()==1);if(applyCooling)applyCooling->setText(format->currentIndex()==0?"应用转速":"应用温控曲线");updateEnabled();}
void MainWindow::updateCurve(){if(!curveChart)return;curveChart->points={};for(int i=0;i<4;i++)curveChart->points.append(QJsonObject{{"temperature",curveTemps[i]->value()},{"rpm",curveRPMs[i]->value()}});curveChart->update();}
void MainWindow::lightingFields(){if(!colorButton||!speedField||!applyRGB)return;colorButton->setVisible(colorMode->currentIndex()==1);colorButton->setStyleSheet(QString("background:%1;color:%2;border:1px solid #687080;").arg(selectedColor.name()).arg(selectedColor.lightness()>140?"#121519":"#ffffff"));int effect=0;for(int i=0;i<8;i++)if(effectButtons[i]&&effectButtons[i]->isChecked())effect=i+1;speedField->setVisible(bounds(effect).second>0);updateEnabled();}
void MainWindow::updateEnabled(){
    if(!applyCooling||!applyRGB||!applyLayout||!saveDevice)return;
    const bool ready=!busy&&!screenBusy,hardware=ready&&connected&&!demo;
    for(auto editor:editors)editor->setEnabled(ready&&connected);
    for(int i=0;i<4;i++)slotButtons[i]->setEnabled(ready&&connected&&(demo||presetsLoaded&&!presets[i].isEmpty()));
    applyCooling->setEnabled(ready&&connected&&(demo?format->currentIndex()==0:coolingLoaded&&(format->currentIndex()==1||presetsLoaded)));readPreset->setEnabled(hardware);
    bool effectSelected=false;for(auto effect:effectButtons)if(effect&&effect->isChecked())effectSelected=true;
    applyRGB->setEnabled(hardware&&rgbLoaded&&effectSelected);colorMode->setEnabled(hardware&&rgbLoaded);brightness->setEnabled(hardware&&rgbLoaded);speed->setEnabled(hardware&&rgbLoaded);colorButton->setEnabled(hardware&&rgbLoaded);for(auto effect:effectButtons)if(effect)effect->setEnabled(hardware&&rgbLoaded);
    overviewLCD->setEnabled(hardware&&lcdLoaded);overviewRGB->setEnabled(hardware&&rgbSwitchLoaded);rgbOn->setEnabled(hardware&&rgbSwitchLoaded);rgbOff->setEnabled(hardware&&rgbSwitchLoaded);
    lcdOn->setEnabled(hardware&&lcdLoaded);lcdOff->setEnabled(hardware&&lcdLoaded);bool layoutReady=true;for(auto row:rows)layoutReady&=row->currentIndex()>0;applyLayout->setEnabled(hardware&&layoutReady);
    chooseImage->setEnabled(ready);applyImage->setEnabled(hardware&&!draftID.isEmpty());restoreImage->setEnabled(hardware&&canRestore);
    smart->setEnabled(hardware&&deviceLoaded);powerOn->setEnabled(hardware&&deviceLoaded);saveDevice->setEnabled(hardware&&deviceLoaded);syncInfo->setEnabled(ready&&!demo);autostart->setEnabled(ready&&!demo&&settingsLoaded);
    if(trayAutostart){trayAutostart->setEnabled(autostart->isEnabled());trayAutostart->setChecked(autostart->isChecked());}
}
void MainWindow::renderOutputs(){
    QSignalBlocker b1(overviewLCD),b2(overviewRGB);overviewLCD->setChecked(lcdLoaded&&lcdEnabled);overviewRGB->setChecked(rgbSwitchLoaded&&rgbEnabled);overviewLCD->setText(lcdLoaded?(lcdEnabled?"已开启":"已关闭"):"未读取");overviewRGB->setText(rgbSwitchLoaded?(rgbEnabled?"已开启":"已关闭"):"未读取");lcdOn->setChecked(lcdLoaded&&lcdEnabled);lcdOff->setChecked(lcdLoaded&&!lcdEnabled);rgbOn->setChecked(rgbSwitchLoaded&&rgbEnabled);rgbOff->setChecked(rgbSwitchLoaded&&!rgbEnabled);
}
void MainWindow::setOutput(const QString &kind,bool on){
    if(!connected||demo||busy||screenBusy||(kind=="lcd"?!lcdLoaded:!rgbSwitchLoaded))return;
    mutation("/api/switch",{{"kind",kind},{"on",on}},[this,kind](QJsonObject){if(kind=="lcd")lcdLoaded=false;else rgbSwitchLoaded=false;renderOutputs();nextSync=0;});
}
void MainWindow::mutation(const QString &path,const QJsonObject &body,std::function<void(QJsonObject)> success){
    if(busy||screenBusy)return;busy=true;revision++;const QString key=connectionKey;updateEnabled();message("应用中…");
    api.post(path,body,[this,key,success](ApiResult r){busy=false;if(key!=connectionKey){message("连接已变化，请重试",true);}else if(!r.ok()){message(r.error,true);}else{const bool inactive=r.data.contains("active")&&!r.data.value("active").toBool();message(inactive?"已保存，设备尚未启用此配置":"已应用",inactive);if(success)success(r.data);}nextSync=0;updateEnabled();syncDevice();});
}
void MainWindow::applySlot(int index){
    if(busy||screenBusy||!connected||index<0||index>3)return;
    if(demo){const int speeds[]={1200,2134,2912,3534};mutation("/api/speed",{{"rpm",speeds[index]}},[this,index,speeds](QJsonObject){rendering=true;rpm->setValue(speeds[index]);rendering=false;});return;}
    if(!presetsLoaded)return;busy=true;revision++;const QString key=connectionKey;const bool curve=format->currentIndex()==1;updateEnabled();message("切换档位…");
    api.post("/api/query",{{"kind","cooling-preset"},{"curve",curve},{"slot",index+1}},[this,key,index,curve](ApiResult r){
        const auto c=r.data.value("config").toObject();if(key!=connectionKey||!r.ok()||c.value("slot").toInt()!=index+1||c.contains("points")!=curve){busy=false;message(!r.error.isEmpty()?r.error:"档位读取未匹配，请重试",true);updateEnabled();return;}
        api.post("/api/cooling",c,[this,key](ApiResult result){busy=false;if(key!=connectionKey){message("连接已变化，请重试",true);}else if(!result.ok()){message(result.error,true);}else if(result.data.value("active").toBool()){coolingDirty=false;showCooling(result.data.value("config").toObject(),true);message("已应用");}else message("档位已保存，设备尚未启用此配置",true);nextSync=0;updateEnabled();syncDevice();});
    });
}
void MainWindow::applyManualCooling(){
    QJsonObject config{{"source",source->currentIndex()},{"slot",slot->currentIndex()+1}};
    if(format->currentIndex()==1){QJsonArray points;for(int i=0;i<4;i++){if(i>0&&curveTemps[i]->value()<=curveTemps[i-1]->value()){message("曲线温度需要逐点递增",true);return;}points.append(QJsonObject{{"temperature",curveTemps[i]->value()},{"rpm",curveRPMs[i]->value()}});}config.insert("points",points);}else config.insert("rpm",rpm->value());
    if(!demo&&format->currentIndex()==0){
        if(!presetsLoaded){message("档位尚未读取，请稍候",true);return;}
        const int index=slot->currentIndex(),target=rpm->value();
        if(index>0&&target<=presets[index-1].value("rpm").toInt()){
            message(QString("%1 档转速需高于 %2 档的 %3 RPM").arg(index+1).arg(index).arg(presets[index-1].value("rpm").toInt()),true);return;
        }
        if(index<3&&target>=presets[index+1].value("rpm").toInt()){
            message(QString("%1 档转速需低于 %2 档的 %3 RPM").arg(index+1).arg(index+2).arg(presets[index+1].value("rpm").toInt()),true);return;
        }
    }
    mutation(demo?"/api/speed":"/api/cooling",demo?QJsonObject{{"rpm",rpm->value()}}:config,[this](QJsonObject r){coolingDirty=false;if(r.value("active").toBool())showCooling(r.value("config").toObject(),true);presetsLoaded=false;nextSync=0;});
}
void MainWindow::setLighting(){
    QJsonObject patch;int effect=0;for(int i=0;i<8;i++)if(effectButtons[i]&&effectButtons[i]->isChecked())effect=i+1;
    if(rgbDirty.contains("effect")&&effect>0)patch.insert("effect",effectIDs[effect-1]);if(rgbDirty.contains("colorMode"))patch.insert("colorMode",colorMode->currentIndex()==0?"multi":"single");if(rgbDirty.contains("color")&&colorMode->currentIndex()==1)patch.insert("color",selectedColor.name());if(rgbDirty.contains("brightness"))patch.insert("brightness",brightness->value());if(rgbDirty.contains("speed")&&bounds(effect).second>0)patch.insert("speed",speed->value());if(patch.isEmpty())patch.insert("brightness",brightness->value());mutation("/api/rgb",patch,[this](QJsonObject data){rgbDirty.clear();applyReadback("rgb",data);});
}
void MainWindow::prepareImage(){
    if(busy||screenBusy)return;const auto path=QFileDialog::getOpenFileName(this,"选择屏保图片",{},"图片 (*.png *.jpg *.jpeg)");if(path.isEmpty())return;QFile file(path);if(!file.open(QIODevice::ReadOnly)||file.size()>8*1024*1024){message("请选择 8 MB 以内的 PNG 或 JPG 图片",true);return;}const auto bytes=file.readAll();mutation("/api/screensaver/prepare",{{"image",QString::fromLatin1(bytes.toBase64())}},[this](QJsonObject data){draftID=data.value("draftId").toString();QPixmap image;image.loadFromData(QByteArray::fromBase64(data.value("preview").toString().toLatin1()));preview->setPixmap(image.scaled(preview->width()-24,190,Qt::KeepAspectRatio,Qt::SmoothTransformation));message("");});
}

bool MainWindow::eventFilter(QObject *watched,QEvent *event){
    auto widget=qobject_cast<QWidget*>(watched);
    if(widget&&widget->window()==this&&!isMaximized()&&(event->type()==QEvent::MouseMove||event->type()==QEvent::MouseButtonPress)){
        auto mouse=static_cast<QMouseEvent*>(event);const auto point=widget->mapTo(this,mouse->position().toPoint());Qt::Edges edges;
        if(point.x()<6)edges|=Qt::LeftEdge;else if(point.x()>width()-7)edges|=Qt::RightEdge;
        if(point.y()<6)edges|=Qt::TopEdge;else if(point.y()>height()-7)edges|=Qt::BottomEdge;
        if(event->type()==QEvent::MouseMove){if(edges==(Qt::LeftEdge|Qt::TopEdge)||edges==(Qt::RightEdge|Qt::BottomEdge))setCursor(Qt::SizeFDiagCursor);else if(edges==(Qt::RightEdge|Qt::TopEdge)||edges==(Qt::LeftEdge|Qt::BottomEdge))setCursor(Qt::SizeBDiagCursor);else if(edges.testFlag(Qt::LeftEdge)||edges.testFlag(Qt::RightEdge))setCursor(Qt::SizeHorCursor);else if(edges.testFlag(Qt::TopEdge)||edges.testFlag(Qt::BottomEdge))setCursor(Qt::SizeVerCursor);else unsetCursor();}
        else if(mouse->button()==Qt::LeftButton&&edges&&windowHandle())return windowHandle()->startSystemResize(edges);
    }
    return QWidget::eventFilter(watched,event);
}
