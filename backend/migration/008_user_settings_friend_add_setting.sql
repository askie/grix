ALTER TABLE user_settings
    ADD COLUMN friend_add_setting SMALLINT NOT NULL DEFAULT 1;

ALTER TABLE user_settings
    ADD CONSTRAINT chk_user_settings_friend_add_setting
    CHECK (friend_add_setting IN (1, 2, 3));
