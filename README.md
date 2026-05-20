# Prophylaxis Scheduler

Минимальный HTTP-сервис на Go: хранит профилактики в YAML, ходит на хост через bastion по SSH и запускает команду.

## Запуск

```bash
go mod tidy
go run .
```

Переменные окружения:

- `ADDR` - адрес сервера, по умолчанию `:8080`
- `DATA_FILE` - путь к YAML, по умолчанию `data/maintenances.yaml`
- `COMMAND_TIMEOUT` - таймаут одной команды, по умолчанию `2m`

## Ручки

Добавить профилактику:

```bash
curl -X POST http://localhost:8080/maintenances \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "Check nginx",
    "active": true,
    "command": "systemctl status nginx --no-pager",
    "host": {
      "address": "10.0.2.15",
      "port": 22,
      "user": "app",
      "auth": {"private_key_path": "/Users/me/.ssh/id_ed25519"}
    },
    "bastion": {
      "address": "bastion.example.com",
      "port": 22,
      "user": "jump",
      "auth": {"password": "change-me"}
    }
  }'
```

Список:

```bash
curl http://localhost:8080/maintenances
```

Активировать / деактивировать:

```bash
curl -X POST http://localhost:8080/maintenances/nginx-status/activate
curl -X POST http://localhost:8080/maintenances/nginx-status/deactivate
```

Запустить все активные профилактики параллельно:

```bash
curl -X POST http://localhost:8080/maintenances/run
```
