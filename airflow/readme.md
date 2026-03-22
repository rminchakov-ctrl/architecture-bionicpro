docker-compose exec airflow-webserver bash
pip list | grep clickhouse

# Зайдем в контейнер
docker-compose exec airflow-webserver bash

# Добавим коннект
docker-compose exec airflow-webserver airflow connections add 'clickhouse_default' \
    --conn-type 'generic' \
    --conn-host 'clickhouse' \
    --conn-port '9000' \
    --conn-login 'default' \
    --conn-password 'default_pwd' \
    --conn-extra '{"driver": "clickhouse", "database": "bionicpro"}'

# Перезапустим
docker-compose restart airflow-webserver airflow-scheduler

# Настройки
docker-compose exec olap_db cat /etc/clickhouse-server/users.xml

docker-compose exec olap_db clickhouse-client --user default --password "" 
ALTER USER default IDENTIFIED WITH sha256_password BY 'default_pwd';

# Проверим
docker-compose logs olap_db | grep -i "password\|auth"

docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' airflow-olap_db-1
    *172.20.0.3*
docker-compose exec airflow-webserver python -c "
from clickhouse_driver import Client
try:
    client = Client(
        host='172.20.0.3', 
        port=9000,
        user='default', 
        password='default_pwd',
        database='bionicpro'
    )
    result = client.execute('SELECT currentDatabase()')
    print('Текущая БД:', result[0][0])
    result = client.execute('SELECT 1')
    print('Успешное подключение:', result)
except Exception as e:
    print('Ошибка подключения:', e)
"