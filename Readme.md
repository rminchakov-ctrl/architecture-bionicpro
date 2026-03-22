# Задание 1. Повышение безопасности системы

## Результаты
- [Диаграмма архитектуры системы](_docs/BionicPRO_C4_model.drawio)
- Код в репозитории, реализующий PKCE flow.
- Код нового бэкенд-сервиса, который реализует получение access- и refresh-токенов и генерации пользовательской сессии.
- файл [keycloak/keycloak-results-export.json](keycloak/keycloak-results-export.json)

## Замечания
без правки руками в базе keycloak_db на порту отличном от 8080 не работает keycloak:
```bash
docker exec -it architecture-bionicpro-keycloak_db-1 psql -U keycloak_user -d keycloak_db -c "UPDATE realm SET ssl_required = 'NONE';"
```
(отключается запрос https)

# Задание 2. Разработка сервиса отчётов

## Результаты
- [Диаграмма архитектуры системы](_docs/report.drawio)
- [Описание решения](_docs/Readme.md)
- Код в репозитории, связанный с Airflow в отдельную папку (airflow).
- Добавлена [имплементация для API](bionicpro-auth/handlers/report.go) для фактического сбора данных из ClickHouse.

# Задание 3. Снижение нагрузки на базу данных

## Результаты
- Добавлен в [сервис API](bionicpro-auth/handlers/report.go) код схемы взаимодействия с S3 и CDN, указанной в задании.
- Добавлен [файл конфигурации Nginx](nginx/nginx.conf) с настройками reverse proxy в отдельную папку nginx.
- Добавлен в docker-compose файл конфигурации развёртывания Nginx.

# Задание 4. Повышение оперативности и стабильности работы CRM

## Результаты
- Добавлен файл конфигурации развёртывания Kafka и kafka-connect в docker compose.
- Добавлен файл конфигурации debezium-connector захвата данных из БД CRM в папку debezium.
- Добавлены скрипты приёма данных через механизм KafkaEngine и код создания MaterializedView для витрины в Clickhouse.