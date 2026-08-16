ALTER TABLE reading_progress ADD COLUMN locator TEXT;
ALTER TABLE reading_progress ADD COLUMN fraction REAL CHECK (fraction IS NULL OR (fraction >= 0 AND fraction <= 1));
