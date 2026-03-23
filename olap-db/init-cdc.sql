USE bionicpro;

-- Таблица для приема данных из Kafka
CREATE TABLE IF NOT EXISTS kafka_customers (
    _key String,
    _topic String,
    _partition UInt64,
    _offset UInt64,
    _timestamp DateTime,
    _source String,
    op String,
    before String,
    after String,
    ts_ms UInt64
) ENGINE = Kafka(
    'kafka:9092',
    'crm-db-server.public.customers',
    'clickhouse-consumer-group',
    'JSONEachRow'
);

-- Целевая таблица для данных customers
CREATE TABLE IF NOT EXISTS customers_cdc (
    id UInt64,
    name String,
    email String,
    age Nullable(Int16),
    gender Nullable(String),
    country Nullable(String),
    address Nullable(String),
    phone Nullable(String),
    _version UInt64 DEFAULT 0,
    _deleted UInt8 DEFAULT 0,
    _timestamp DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(_version, _deleted)
ORDER BY id;

-- Materialized View для обработки данных
CREATE MATERIALIZED VIEW IF NOT EXISTS customers_cdc_mv 
TO customers_cdc AS
SELECT 
    JSONExtractUInt(after, 'id') as id,
    JSONExtractString(after, 'name') as name,
    JSONExtractString(after, 'email') as email,
    JSONExtractInt(after, 'age') as age,
    JSONExtractString(after, 'gender') as gender,
    JSONExtractString(after, 'country') as country,
    JSONExtractString(after, 'address') as address,
    JSONExtractString(after, 'phone') as phone,
    toUnixTimestamp(now()) as _version,
    0 as _deleted,
    now() as _timestamp
FROM kafka_customers
WHERE op IN ('c', 'u');

-- Витрина данных для отчетности
CREATE TABLE IF NOT EXISTS customer_reporting_mart (
    customer_id UInt64,
    customer_name String,
    customer_email String,
    age_group String,
    gender String,
    country String,
    data_completeness Float32,
    created_date Date,
    last_updated DateTime
) ENGINE = ReplacingMergeTree(last_updated)
ORDER BY (country, age_group, gender);

-- MV для заполнения витрины
CREATE MATERIALIZED VIEW customer_reporting_mv 
TO customer_reporting_mart AS
SELECT 
    id as customer_id,
    name as customer_name,
    email as customer_email,
    CASE 
        WHEN age < 20 THEN 'Under 20'
        WHEN age < 30 THEN '20-29'
        WHEN age < 40 THEN '30-39'
        WHEN age < 50 THEN '40-49'
        ELSE '50+'
    END as age_group,
    gender,
    country,
    (if(name != '', 1, 0) + if(email != '', 1, 0) + if(country != '', 1, 0)) / 3.0 as data_completeness,
    toDate(_timestamp) as created_date,
    now() as last_updated
FROM customers_cdc
WHERE _deleted = 0;