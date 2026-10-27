#pragma once
#include "api_client.h"
#include <QWidget>
#include <QJsonArray>
#include <QSet>
#include <QTimer>
#include <QColor>
#include <array>

class QLabel; class QPushButton; class QComboBox; class QSpinBox; class QSlider; class QCheckBox; class QProgressBar; class QStackedWidget;
class Gauge; class MetricCard; class CurveChart;
class QSystemTrayIcon; class QAction; class QCloseEvent;
class MainWindow : public QWidget {
public:
    explicit MainWindow(const QUrl &base, QWidget *parent = nullptr);
    void activate();
    void previewPage(int index);
    void enableTray(bool startHidden=false);
protected:
    bool eventFilter(QObject *watched, QEvent *event) override;
    void closeEvent(QCloseEvent *event) override;
private:
    ApiClient api;
    QSystemTrayIcon *tray=nullptr;
    QAction *trayAutostart=nullptr, *trayExit=nullptr;
    bool settingsLoaded=false;
    QTimer pollTimer;
    QString connectionKey, draftID;
    bool connected=false, demo=false, busy=false, screenBusy=false, rendering=false, polling=false, syncing=false, screenPolling=false, auxiliaryPolling=false;
    bool coolingLoaded=false, rgbLoaded=false, deviceLoaded=false, lcdLoaded=false, rgbSwitchLoaded=false, lcdEnabled=false, rgbEnabled=false, presetsLoaded=false, canRestore=false;
    bool coolingDirty=false, deviceDirty=false;
    QSet<QString> rgbDirty;
    QJsonObject currentCooling;
    std::array<QJsonObject,4> presets;
    qint64 nextSync=0;
    int statusSequence=0, revision=0;
    QLabel *pageHeading, *badge, *transport, *feedback, *current, *preview, *screenState, *layoutState, *connectionDetail;
    Gauge *gauge; MetricCard *cpuCard, *gpuCard; CurveChart *curveChart;
    QStackedWidget *pages;
    std::array<QPushButton*,4> slotButtons;
    std::array<QPushButton*,8> effectButtons{};
    QComboBox *format, *slot, *source, *colorMode;
    QSpinBox *rpm;
    QSlider *rpmSlider, *brightness, *speed;
    QLabel *brightnessValue, *speedValue;
    QWidget *fixedEditor, *curveEditor, *advancedEditor, *speedField;
    std::array<QSpinBox*,4> curveTemps, curveRPMs;
    QPushButton *applyCooling, *applyRGB, *colorButton, *lcdOn, *lcdOff, *applyLayout, *chooseImage, *applyImage, *restoreImage, *saveDevice, *readPreset, *refreshDevice;
    std::array<QComboBox*,3> rows;
    QCheckBox *overviewLCD, *overviewRGB;
    QPushButton *rgbOn, *rgbOff;
    QCheckBox *smart, *powerOn, *syncInfo, *autostart;
    QProgressBar *progress;
    QColor selectedColor{0,255,72};
    QList<QWidget*> editors;
    QWidget *dashboard(); QWidget *lighting(); QWidget *screen(); QWidget *settings();
    void poll(); void syncDevice(); void syncKind(int index, QString key, int epoch); void loadPresets(int index, QString key, bool curve, int epoch);
    void applyReadback(const QString &kind, const QJsonObject &data);
    void resetConnection(QString key); void updateEnabled(); void renderPresets(); void showCooling(const QJsonObject &config, bool active);
    void coolingFields(); void lightingFields(); void invalidatePresets(); void updateCurve();
    void setOutput(const QString &kind, bool on); void renderOutputs();
    void applySlot(int index); void applyManualCooling(); void setLighting(); void prepareImage();
    void mutation(const QString &path, const QJsonObject &body, std::function<void(QJsonObject)> success = {});
    void message(const QString &text, bool error = false);
};
