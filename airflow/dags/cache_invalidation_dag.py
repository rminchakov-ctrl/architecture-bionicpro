from airflow import DAG
from airflow.operators.python import PythonOperator
from datetime import datetime, timedelta
import boto3
from botocore.client import Config
from botocore.exceptions import ClientError, NoCredentialsError
import logging

default_args = {
    'owner': 'airflow',
    'start_date': datetime(2024, 1, 1),
    'retries': 2,
    'retry_delay': timedelta(minutes=5),
    'depends_on_past': False,
}

def invalidate_old_reports():
    """
    Удаляет отчеты из S3, которые старше 7 дней
    """
    try:
        # Инициализация клиента S3 для MinIO
        s3 = boto3.client(
            's3',
            endpoint_url='http://minio:9000',
            aws_access_key_id='minioadmin',
            aws_secret_access_key='minioadmin',
            config=Config(signature_version='s3v4'),
            region_name='us-east-1'
        )
        
        bucket_name = 'reports'
        cutoff_date = datetime.now() - timedelta(days=7)
        
        logging.info(f"Starting invalidation of reports older than {cutoff_date}")
        
        # Получаем список объектов в bucket
        paginator = s3.get_paginator('list_objects_v2')
        deletion_list = []
        
        for page in paginator.paginate(Bucket=bucket_name):
            if 'Contents' in page:
                for obj in page['Contents']:
                    last_modified = obj['LastModified'].replace(tzinfo=None)
                    if last_modified < cutoff_date:
                        deletion_list.append({'Key': obj['Key']})
                        logging.info(f"Marked for deletion: {obj['Key']} (last modified: {last_modified})")
        
        # Удаляем старые объекты пачками (S3 позволяет до 1000 объектов за раз)
        if deletion_list:
            for i in range(0, len(deletion_list), 1000):
                batch = deletion_list[i:i + 1000]
                s3.delete_objects(
                    Bucket=bucket_name,
                    Delete={'Objects': batch}
                )
                logging.info(f"Deleted batch of {len(batch)} objects")
            
            logging.info(f"Total deleted objects: {len(deletion_list)}")
        else:
            logging.info("No old reports found for deletion")
            
    except NoCredentialsError:
        logging.error("S3 credentials not found")
        raise
    except ClientError as e:
        logging.error(f"S3 client error: {e}")
        raise
    except Exception as e:
        logging.error(f"Unexpected error: {e}")
        raise

def cleanup_empty_folders():
    """
    Очищает пустые папки в S3 (S3 не имеет реальных папок, но префиксы)
    """
    try:
        s3 = boto3.client(
            's3',
            endpoint_url='http://minio:9000',
            aws_access_key_id='minioadmin',
            aws_secret_access_key='minioadmin',
            config=Config(signature_version='s3v4'),
            region_name='us-east-1'
        )
        
        bucket_name = 'reports'
        
        # Получаем список префиксов (папок)
        result = s3.list_objects_v2(Bucket=bucket_name, Delimiter='/')
        
        if 'CommonPrefixes' in result:
            for prefix in result['CommonPrefixes']:
                folder_name = prefix['Prefix']
                # Проверяем, пустая ли папка
                objects_in_folder = s3.list_objects_v2(
                    Bucket=bucket_name,
                    Prefix=folder_name,
                    MaxKeys=1
                )
                
                if 'Contents' not in objects_in_folder:
                    # Папка пустая, но в S3 нет реальных папков, поэтому просто логируем
                    logging.info(f"Empty folder (prefix) found: {folder_name}")
                    # В S3 папки создаются автоматически при наличии объектов с префиксом
                    # и удаляются автоматически когда объектов нет
                    
    except Exception as e:
        logging.warning(f"Folder cleanup warning: {e}")

with DAG('cache_invalidation_dag',
         default_args=default_args,
         schedule_interval='@daily',  # Запуск каждый день
         catchup=False,
         description='Invalidate old reports from S3 storage',
         tags=['maintenance', 's3']) as dag:

    invalidate_task = PythonOperator(
        task_id='invalidate_old_reports',
        python_callable=invalidate_old_reports,
        dag=dag,
    )

    cleanup_task = PythonOperator(
        task_id='cleanup_empty_folders',
        python_callable=cleanup_empty_folders,
        dag=dag,
    )

    # Определяем порядок выполнения
    invalidate_task >> cleanup_task