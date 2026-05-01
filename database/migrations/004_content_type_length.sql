-- Migration: 004_content_type_length.sql
-- Increase content_type column size to accommodate system message types

ALTER TABLE messages ALTER COLUMN content_type TYPE VARCHAR(50);
