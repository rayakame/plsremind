CREATE TABLE dm_channels (
    account_id integer NOT NULL,
    bot_id integer NOT NULL,
    channel_id integer NOT NULL,
    PRIMARY KEY (account_id, bot_id)
);
CREATE INDEX messages_account_idx ON messages (account_id);
