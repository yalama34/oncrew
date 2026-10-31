# OnCrew

Интерфейс для управления запусками и просмотра работы команды агентов.

## Запуск

1. В корневом `.env` задайте `DB_USER`, `DB_PASSWORD`, `POSTGRES_DB`, `LLM_API_KEY`, `LLM_BASE_URL` и `LLM_MODEL`.
2. В корне проекта выполните `docker compose up --build -d`.
3. Откройте http://localhost:3000. API доступен на http://localhost:8080.

Остановить сервисы: `docker compose down`. Данные PostgreSQL сохраняются в томе `postgres_data`.

Для разработки без Docker frontend можно запустить командой `npm start`; запросы `/api` проксируются на `http://localhost:8080`. При необходимости другой адрес API задаётся через `REACT_APP_API_URL` до запуска frontend.

В OnCrew можно создавать агентов и запуски, просматривать задачи и артефакты. Выполнение запроса к платной модели может расходовать средства аккаунта провайдера.
