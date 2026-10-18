CREATE TABLE notes (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL
);
