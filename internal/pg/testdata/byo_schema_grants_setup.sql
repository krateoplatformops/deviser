-- Scenario "schema-grants": least-privilege alternative to database ownership.
-- Not part of the public guide; kept as engineering evidence that deviser
-- needs neither superuser nor CREATE EXTENSION privileges.
-- Run on the maintenance database as an administrative user.
CREATE ROLE krateo_app_lp LOGIN PASSWORD 'krateo_app_lp' NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE DATABASE krateo_db_lp;
