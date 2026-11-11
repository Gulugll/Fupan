-- V0.6 卡片分类：单字段起步（tags 表等有真实需求再上）。
ALTER TABLE cards ADD COLUMN category TEXT NOT NULL DEFAULT '';
