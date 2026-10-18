CREATE TABLE notes (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL
);

CREATE INDEX notes_created_at_id ON notes (created_at, id);
