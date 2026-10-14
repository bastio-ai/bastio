DROP TABLE IF EXISTS workspace_setup_review;
DROP TABLE IF EXISTS workspace_model_test_limits;
DROP TABLE IF EXISTS workspace_model_checks;
DROP TABLE IF EXISTS workspace_provider_models;
ALTER TABLE workspace_conversations DROP COLUMN IF EXISTS is_setup_test;
