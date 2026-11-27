-- +goose Up
-- Short links to the approval page of the terminal client (ADR-0029): the terminal registers its request and shows
-- /cli/a/<code>, which fits on one line and can be typed on another device, instead of an address of about 200 characters. The code
-- is 8 characters without lookalikes; it points to a request and grants nothing by itself, and it expires in 10 minutes.
CREATE TABLE cli_links (
    code text PRIMARY KEY,
    code_challenge text NOT NULL,
    redirect_uri text NOT NULL DEFAULT '',
    state text NOT NULL DEFAULT '',
    client_name text NOT NULL,
    client_host text NOT NULL,
    ip text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX cli_links_expires_at_idx ON cli_links (expires_at);

-- +goose Down
DROP TABLE cli_links;
