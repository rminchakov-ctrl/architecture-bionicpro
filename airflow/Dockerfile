FROM apache/airflow:2.9.1-python3.9

# Копируем requirements.txt
USER root
COPY /airflow/requirements.txt /tmp/requirements.txt
RUN chown airflow:root /tmp/requirements.txt

# Устанавливаем зависимости от имени airflow
USER airflow
RUN pip install --upgrade pip && \
    pip install --no-cache-dir -r /tmp/requirements.txt

# Выполняем миграции базы данных для регистрации providers
RUN airflow db migrate

# Создаем необходимые директории
RUN mkdir -p /opt/airflow/dags /opt/airflow/logs