-- Baseline. The migration journal is created by the runner in the same transaction.
-- Domain owners add their own tables in later, immutable migrations.
SELECT 1;
