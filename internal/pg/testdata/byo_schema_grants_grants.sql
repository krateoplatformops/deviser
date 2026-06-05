-- Scenario "schema-grants": grants for the application role.
-- Run INSIDE the target database (krateo_db_lp) as an administrative user.
-- CONNECT plus USAGE/CREATE on the public schema is enough for all deviser
-- DDL (tables, indexes, functions, triggers) but not for CREATE EXTENSION.
GRANT CONNECT ON DATABASE krateo_db_lp TO krateo_app_lp;
GRANT USAGE, CREATE ON SCHEMA public TO krateo_app_lp;
