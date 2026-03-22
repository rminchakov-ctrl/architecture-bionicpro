- в realm-export.json Заменить your-yandex-client-id и your-yandex-client-secret на реальные значения из вашего приложения в Яндекс OAuth
- Настроить redirect URI в Яндекс OAuth: http://localhost:8080/realms/reports-realm/broker/yandex/endpoint

без 
docker exec -it architecture-bionicpro-keycloak_db-1 psql -U keycloak_user -d keycloak_db -c "UPDATE realm SET ssl_required = 'NONE';"
на порту отличном от 8080 не работает keycloak