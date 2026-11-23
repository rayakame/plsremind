CREATE TABLE dm_channels (
    bot_id integer NOT NULL,
    account_id integer NOT NULL,
    channel_id integer NOT NULL,
    PRIMARY KEY (bot_id, account_id)
);
CREATE INDEX messages_account_idx ON messages (account_id);
