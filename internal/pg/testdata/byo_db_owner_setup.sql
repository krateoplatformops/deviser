-- Scenario "db-owner": the documented Bring Your Own PostgreSQL setup.
-- Keep VERBATIM in sync with the krateo-v2-docs guide:
--   docs/30-how-to-guides/50-manage-postgresql/40-bring-your-own-postgresql.md (step 1)
CREATE USER "krateo-db-user" WITH ENCRYPTED PASSWORD 'your_password';
CREATE DATABASE "krateo-db" OWNER "krateo-db-user";
