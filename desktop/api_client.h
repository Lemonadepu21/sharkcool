#pragma once
#include <QNetworkAccessManager>
#include <QJsonObject>
#include <QUrl>
#include <functional>
#include <vector>

struct ApiResult { QJsonObject data; QString error; bool ok() const { return error.isEmpty(); } };
class ApiClient : public QObject {
public:
    using Callback = std::function<void(ApiResult)>;
    explicit ApiClient(QUrl base, QObject *parent = nullptr);
    void get(const QString &path, Callback callback);
    void post(const QString &path, const QJsonObject &body, Callback callback);
private:
    QNetworkAccessManager network;
    QUrl base;
    QByteArray token;
    bool tokenLoading = false;
    std::vector<std::function<void(QString)>> tokenWaiters;
    void ensureToken(std::function<void(QString)> callback);
    void request(const QString &path, const QJsonObject &body, bool post, Callback callback, bool retried = false);
};
