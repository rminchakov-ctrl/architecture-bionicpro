USE bionicpro;

-- Таблица для приема данных из Kafka
CREATE  TABLE IF NOT EXISTS kafka_customers (
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
CREATE  TABLE IF NOT EXISTS customers_cdc (
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
ORDER   BY id;

-- Materialized View для обработки данных
CREATE MATERIALIZED VIEW IF NOT EXISTS customers_cdc_mv 
TO      customers_cdc AS
SELECT  if(op = 'd', JSONExtractRaw(before), JSONExtractRaw(after)) AS raw_json,
        CAST(
            raw_json,
            'Tuple(id UInt64, name String, email String, age Nullable(Int16), gender Nullable(String), country Nullable(String), address Nullable(String), phone Nullable(String))'
        ) AS data,
        data.id as id,
        if(op = 'd', '', data.name) as name,
        if(op = 'd', '', data.email) as email,
        if(op = 'd', NULL, data.age) as age,
        if(op = 'd', NULL, data.gender) as gender,
        if(op = 'd', NULL, data.country) as country,
        if(op = 'd', NULL, data.address) as address,
        if(op = 'd', NULL, data.phone) as phone,    
        (_offset + _partition * 1000000000000) as _version,
        (op = 'd') as _deleted,
        now() as _timestamp
FROM    kafka_customers
WHERE   op IN ('c', 'u', 'd');

-- Витрина данных для отчетности
CREATE  TABLE IF NOT EXISTS customer_reporting_mart (
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
ORDER   BY (country, age_group, gender);

-- MV для заполнения витрины
CREATE  MATERIALIZED VIEW IF NOT EXISTS customer_reporting_mv 
TO      customer_reporting_mart AS
SELECT  id as customer_id,
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
FROM    customers_cdc_mv
WHERE   _deleted = 0;

CREATE  DICTIONARY IF NOT EXISTS customers_dict (
        customer_id UInt64,
        customer_name String,
        customer_email String,
        age_group String,
        gender String,
        country String
)
PRIMARY KEY customer_id
SOURCE(CLICKHOUSE(TABLE 'customer_reporting_mart'))
LIFETIME(MIN 60 MAX 600)
LAYOUT(HASHED());

-- итоговая таблица
CREATE  TABLE IF NOT EXISTS user_reports_summary (
        user_id UInt64,
        customer_name String,
        customer_email String,
        age_group String,
        gender String,
        country String,
        total_signals AggregateFunction(count, UInt64),
        avg_amplitude AggregateFunction(avg, Float64),
        max_amplitude AggregateFunction(max, Float64),
        last_signal_time AggregateFunction(max, DateTime),
        _updated DateTime
) ENGINE = AggregatingMergeTree(_updated)
ORDER   BY user_id;

-- MV для заполнения
CREATE  MATERIALIZED VIEW user_reports_summary_mv
TO      user_reports_summary
SELECT  user_id,
        -- Используем any(), так как для одного ID значения из словаря будут одинаковыми    
        any(dictGetOrDefault('customers_dict', 'customer_name', user_id)) as customer_name,
        any(dictGetOrDefault('customers_dict', 'customer_email', user_id)) as customer_email,
        any(dictGetOrDefault('customers_dict', 'age_group', user_id)) as age_group,
        any(dictGetOrDefault('customers_dict', 'gender', user_id)) as gender,
        any(dictGetOrDefault('customers_dict', 'country', user_id)) as country,
        countState(1) as total_signals,
        avgState(signal_amplitude) as avg_amplitude,
        maxState(signal_amplitude) as max_amplitude,
        maxState(signal_time) as last_signal_time,
        now() as _updated
FROM    emg_sensor_data
GROUP   BY user_id;