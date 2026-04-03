## Mattermost Bot Button Webhook

Этот плагин позволяет настроить webhook, который будет вызываться:

- когда **пользователь пишет сообщение боту** (по User ID бота) в Mattermost
- когда пользователь **нажимает интерактивную кнопку** в сообщении (см. обработчик `handleButtonClick`)

В webhook отправляется JSON полезной нагрузки (для сообщений боту — `model.Post`). Остальные сообщения, а также сообщения, отправленные самим ботом, игнорируются.

## Установка

Соберите плагин командой `make dist` и загрузите архив из папки `dist` в Mattermost (System Console → Plugins → Plugin Management).

> [!IMPORTANT]
> Если загрузка плагина не проходит, проверьте лимиты на размер загружаемых файлов:
>
> - **Mattermost**: параметр *Maximum File Size* в System Console
> - **Nginx**: `client_max_body_size` в конфиге (обычно `/etc/nginx/conf.d/mattermost.conf`)

## Ручная сборка

Клонируйте репозиторий и выполните `make dist`. Готовый архив будет в `dist/`.

## Настройка

После установки настройте параметр `BotWebhooks` в System Console → Plugins → Bot button webhook.

Формат значения:

`bot_id1: https://url1.com, bot_id2: https://url2.com`

Где:

- `bot_id` — User ID бота в Mattermost
- `url` — endpoint вашего webhook
- пары разделяются **запятой**

## Запуск локального сервера обработки вебхуков
lsof -ti :9090 | xargs kill -9 2>/dev/null && go run cmd/webhook-server/main.go

## Перезагрузка плагина

```bash
# Получить токен сессии
TOKEN=$(curl -si 'http://localhost:8065/api/v4/users/login' \
  --header 'Content-Type: application/json' \
  --data '{"login_id":"test","password":"12345678"}' \
  | grep -i "^token:" | awk '{print $2}' | tr -d '\r')

# Отключить плагин
curl -s -X POST "http://localhost:8065/api/v4/plugins/bot-button-webhook/disable" \
  --header "Authorization: Bearer $TOKEN"

# Включить плагин
curl -s -X POST "http://localhost:8065/api/v4/plugins/bot-button-webhook/enable" \
  --header "Authorization: Bearer $TOKEN"
```

## Кнопки в сообщениях: важный момент про `integration.url`

При нажатии на кнопку **запрос на `integration.url` выполняет сам сервер Mattermost**, а не браузер пользователя. Поэтому в `integration.url` должен быть адрес, **доступный с той машины, где запущен Mattermost**:

- Если Mattermost на одном сервере без Docker — используйте `http://localhost:8065/plugins/bot-button-webhook/actions/...` (порт 8065 — стандартный порт Mattermost).
- Если Mattermost за Nginx на том же хосте — подойдёт `http://127.0.0.1/plugins/...` (порт 80) только если Nginx проксирует запросы к Mattermost.
- Если Mattermost в Docker — с контейнера должен быть доступен хост (например `http://host.docker.internal:8065/...` или внутренний IP хоста и порт, на котором слушает Mattermost/Nginx).

Если указать публичный URL вида `http://talk-test.vkusvill.ru/...`, сервер попытается обратиться к нему «снаружи» (по DNS). Часто это приводит к **таймауту** (dial tcp ... i/o timeout), если с сервера до этого адреса нет маршрута или порт закрыт файрволом. В таких случаях переключитесь на внутренний URL (localhost или внутренний хост/порт).

## Отправка сообщения с кнопками через API

```bash
curl --location 'http://localhost:8065/api/v4/posts' \
  --header 'Authorization: Bearer xssyp91zsirzikdxfhmot6ao1c' \
  --header 'Content-Type: application/json' \
  --data '{
    "channel_id": "7omrxkaoq7dqtfdpyjuj36p1or",
    "message": "Требуется подтверждение",
    "props": {
      "attachments": [
        {
          "pretext": "Поступил новый запрос на согласование.",
          "text": "Пожалуйста, выберите действие:",
          "actions": [
            {
              "id": "approve",
              "name": "✅ Одобрить",
              "type": "button",
              "integration": {
                "url": "http://localhost:8065/plugins/bot-button-webhook/actions/approve",
                "context": {
                  "action": "approve"
                }
              }
            },
            {
              "id": "reject",
              "name": "❌ Отклонить",
              "type": "button",
              "integration": {
                "url": "http://localhost:8065/plugins/bot-button-webhook/actions/reject",
                "context": {
                  "action": "reject"
                }
              }
            }
          ]
        }
      ]
    }
  }'
```

## Загрузка новой версии плагина после пересборки

```bash
TOKEN=$(curl -si 'http://localhost:8065/api/v4/users/login' \
  --header 'Content-Type: application/json' \
  --data '{"login_id":"test","password":"12345678"}' \
  | grep -i "^token:" | awk '{print $2}' | tr -d '\r')

make dist && \
curl -s -X POST 'http://localhost:8065/api/v4/plugins' \
  --header "Authorization: Bearer $TOKEN" \
  -F 'plugin=@dist/bot-button-webhook-0.1.0.tar.gz' \
  -F 'force=true' && \
curl -s -X POST "http://localhost:8065/api/v4/plugins/bot-button-webhook/enable" \
  --header "Authorization: Bearer $TOKEN"
```
