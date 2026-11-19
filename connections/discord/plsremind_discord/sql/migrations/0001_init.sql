CREATE TABLE deliveries (
    delivery_id text PRIMARY KEY,
    sent_at timestamp NOT NULL
);

CREATE TABLE messages (
    message_id integer PRIMARY KEY,
    channel_id integer NOT NULL,
    account_id integer NOT NULL,
    user_id text NOT NULL,
    item_id text NOT NULL,
    item_updated_at timestamp,
    sent_at timestamp NOT NULL
);
CREATE INDEX messages_item_idx ON messages (item_id);
CREATE INDEX messages_user_idx ON messages (user_id);
