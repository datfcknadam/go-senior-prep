-- Упрощённая модель объявлений для задач из TASKS.md.
DROP TABLE IF EXISTS ad_views, ads, users, categories CASCADE;

CREATE TABLE users (
    id         bigserial PRIMARY KEY,
    name       text        NOT NULL,
    city       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE categories (
    id        int PRIMARY KEY,
    parent_id int REFERENCES categories (id),
    name      text NOT NULL
);

CREATE TABLE ads (
    id          bigserial PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES users (id),
    category_id int         NOT NULL REFERENCES categories (id),
    title       text        NOT NULL,
    price       bigint      NOT NULL CHECK (price >= 0),
    status      text        NOT NULL CHECK (status IN ('draft', 'active', 'archived')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    version     int         NOT NULL DEFAULT 1
);

CREATE TABLE ad_views (
    ad_id     bigint      NOT NULL REFERENCES ads (id),
    viewer_id bigint,
    viewed_at timestamptz NOT NULL DEFAULT now()
);

-- Тестовые данные.
INSERT INTO categories VALUES
    (1, NULL, 'Транспорт'), (2, 1, 'Автомобили'), (3, 1, 'Мотоциклы'),
    (4, NULL, 'Недвижимость'), (5, 4, 'Квартиры'), (6, 5, 'Новостройки');

INSERT INTO users (name, city)
SELECT 'user_' || g, (ARRAY['Москва', 'Санкт-Петербург', 'Казань', 'Новосибирск'])[1 + g % 4]
FROM generate_series(1, 10000) g;

INSERT INTO ads (user_id, category_id, title, price, status, created_at)
SELECT 1 + (random() * 9999)::int,
       (ARRAY[2, 3, 5, 6])[1 + g % 4],
       'Объявление ' || g,
       (random() * 10000000)::bigint,
       (ARRAY['draft', 'active', 'active', 'active', 'archived'])[1 + g % 5],
       now() - random() * interval '365 days'
FROM generate_series(1, 500000) g;

INSERT INTO ad_views (ad_id, viewer_id, viewed_at)
SELECT 1 + (random() * 499999)::int, 1 + (random() * 9999)::int, now() - random() * interval '30 days'
FROM generate_series(1, 1000000) g;

ANALYZE;
