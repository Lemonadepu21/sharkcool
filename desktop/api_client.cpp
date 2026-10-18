#include "api_client.h"
#include <QNetworkReply>
#include <QNetworkProxy>
#include <QJsonDocument>
#include <QRegularExpression>
#include <QTimer>

ApiClient::ApiClient(QUrl url, QObject *parent) : QObject(parent), network(this), base(std::move(url)) {
    network.setProxy(QNetworkProxy::NoProxy);
}
void ApiClient::get(const QString &path, Callback callback) { request(path, {}, false, std::move(callback)); }
void ApiClient::post(const QString &path, const QJsonObject &body, Callback callback) { request(path, body, true, std::move(callback)); }
void ApiClient::ensureToken(std::function<void(QString)> callback) {
    if (!token.isEmpty()) { callback({}); return; }
    tokenWaiters.push_back(std::move(callback));
    if (tokenLoading) return;
    tokenLoading = true;
    QNetworkRequest req(base.resolved(QUrl("/")));
    req.setAttribute(QNetworkRequest::RedirectPolicyAttribute, QNetworkRequest::ManualRedirectPolicy);
    req.setTransferTimeout(7000);
    auto reply = network.get(req);
    QTimer::singleShot(8000, reply, [reply] { if (!reply->isFinished()) reply->abort(); });
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        const QString page = QString::fromUtf8(reply->readAll());
        const auto match = QRegularExpression("name=\"sharkcool-token\" content=\"([a-f0-9]+)\"").match(page);
        QString error;
        if (reply->error() != QNetworkReply::NoError || !match.hasMatch()) error = "无法连接 SharkCool 服务";
        else token = match.captured(1).toUtf8();
        reply->deleteLater(); tokenLoading = false;
        auto waiters = std::move(tokenWaiters); tokenWaiters.clear();
        for (auto &waiter : waiters) waiter(error);
    });
}
void ApiClient::request(const QString &path, const QJsonObject &body, bool post, Callback callback, bool retried) {
    if (post && token.isEmpty()) {
        ensureToken([this, path, body, callback, retried](QString error) {
            if (!error.isEmpty()) callback({{}, error});
            else request(path, body, true, callback, retried);
        }); return;
    }
    QNetworkRequest req(base.resolved(QUrl(path)));
    req.setAttribute(QNetworkRequest::RedirectPolicyAttribute, QNetworkRequest::ManualRedirectPolicy);
    req.setTransferTimeout(7000);
    if (post) { req.setHeader(QNetworkRequest::ContentTypeHeader, "application/json"); req.setRawHeader("X-SharkCool-Token", token); }
    auto reply = post ? network.post(req, QJsonDocument(body).toJson(QJsonDocument::Compact)) : network.get(req);
    QTimer::singleShot(8000, reply, [reply] { if (!reply->isFinished()) reply->abort(); });
    connect(reply, &QNetworkReply::finished, this, [this, path, body, post, callback, retried, reply] {
        const int status = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        QJsonParseError parse;
        auto doc = QJsonDocument::fromJson(reply->readAll(), &parse);
        const auto data = doc.object();
        if (status == 403 && data.value("error").toString() == "缺少有效的会话令牌" && post && !retried) {
            token.clear(); reply->deleteLater(); request(path, body, post, callback, true); return;
        }
        QString error;
        if (status < 200 || status >= 300 || reply->error() != QNetworkReply::NoError)
            error = data.value("error").toString(reply->errorString());
        else if (parse.error != QJsonParseError::NoError || !doc.isObject()) error = "服务返回了无效数据";
        reply->deleteLater(); callback({data, error, status});
    });
}
