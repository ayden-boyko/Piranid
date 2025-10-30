CREATE TABLE IF NOT EXISTS credentials (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date_created VARCHAR(255) NOT NULL,
    client_id INTEGER NOT NULL,
    username VARCHAR(255) NOT NULL,
    user_email VARCHAR(255) NOT NULL,
    hashed_password VARCHAR(255) NOT NULL,
    client_secret VARCHAR(255) NOT NULL,
    service_id VARCHAR(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS auth_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    date_created VARCHAR(255) NOT NULL,
    authcode VARCHAR(255) NOT NULL,
    expires VARCHAR(255) NOT NULL
);