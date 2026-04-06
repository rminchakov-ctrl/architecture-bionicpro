DO $$
BEGIN
    -- Проверяем, не настроено ли уже
    IF current_setting('wal_level') != 'logical' THEN
        ALTER SYSTEM SET wal_level = logical;
        ALTER SYSTEM SET max_wal_senders = 10;
        ALTER SYSTEM SET max_replication_slots = 10;
        PERFORM pg_reload_conf();
    END IF;
    
    -- Создаем публикацию если не существует
    IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'debezium_publication') THEN
        CREATE PUBLICATION debezium_publication FOR TABLE customers;
    END IF;
    
    -- Создаем слот если не существует
    IF NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = 'debezium_slot') THEN
        PERFORM pg_create_logical_replication_slot('debezium_slot', 'pgoutput');
    END IF;
END$$;