-- +goose Up
CREATE TABLE instance_settings (
    id boolean PRIMARY KEY DEFAULT true CHECK (id),
    name text NOT NULL DEFAULT 'plsremind',
    registration text NOT NULL DEFAULT 'invite' CHECK (registration IN ('open', 'invite'))
);

INSERT INTO instance_settings DEFAULT VALUES;

-- +goose Down
DROP TABLE instance_settings;
