-- Seed for the integration tests and e2e (M1 F1.1). The postgres image loads
-- it on first start. Values are fixed, not now() or random(), so screens
-- and assertions stay stable.

create schema agentable;

create type order_status as enum ('pending', 'running', 'done', 'failed');

-- single-column primary key
create table t_user (
    id         bigserial primary key,
    name       text not null,
    email      text,
    active     boolean not null default true,
    created_at timestamptz not null default now()
);

-- over 5000 rows, for paging and counting; one column per type the grid colours
create table t_order (
    id         bigserial primary key,
    user_id    bigint not null references t_user (id),
    status     order_status not null default 'pending',
    amount     numeric(10, 2) not null,
    paid       boolean,
    meta       jsonb,
    raw        json,
    note       text,
    created_at timestamptz not null default now(),
    deleted_at timestamptz
);

-- composite primary key
create table t_order_item (
    order_id bigint not null references t_order (id),
    line_no  int not null,
    sku_code text not null,
    qty      int not null default 1,
    primary key (order_id, line_no)
);

-- no primary key, only a unique index over not-null columns. The nullable and
-- the partial unique index must not count as a row identity (§8.4).
create table t_sku (
    code    text not null,
    barcode text,
    title   text not null,
    price   numeric(10, 2)
);
create unique index t_sku_code on t_sku (code);
create unique index t_sku_barcode on t_sku (barcode);
create unique index t_sku_title_priced on t_sku (title) where price is not null;

-- no primary key and no unique index; one row carries control characters (§7.6)
create table t_log (
    logged_at timestamptz not null,
    msg       text
);

create view v_paid_order as
    select id, user_id, amount from t_order where paid;

create materialized view mv_order_by_status as
    select status, count(*) as n from t_order group by status;

-- only the parent is listed, not its partitions (§8.4)
create table t_event (
    id          bigint not null,
    occurred_at timestamptz not null,
    kind        text not null,
    primary key (id, occurred_at)
) partition by range (occurred_at);
create table t_event_2025 partition of t_event for values from ('2025-01-01') to ('2026-01-01');
create table t_event_2026 partition of t_event for values from ('2026-01-01') to ('2027-01-01');

create table agentable.agent (
    id   serial primary key,
    name text not null unique
);

create table agentable.agent_version (
    agent_id int not null references agentable.agent (id),
    version  int not null,
    released boolean not null default false,
    primary key (agent_id, version)
);

create table agentable.goal (
    id       serial primary key,
    agent_id int references agentable.agent (id),
    title    text not null
);

insert into t_user (name, email, active, created_at)
select 'user_' || i,
       case when i % 5 = 0 then null else 'user_' || i || '@example.com' end,
       i % 7 <> 0,
       timestamptz '2026-01-01 00:00:00+00' + i * interval '1 day'
from generate_series(1, 50) i;

insert into t_order (user_id, status, amount, paid, meta, raw, note, created_at, deleted_at)
select 1 + i % 50,
       (array['pending', 'running', 'done', 'failed'])[1 + i % 4]::order_status,
       (i % 1000) + 0.99,
       case when i % 3 = 0 then null else i % 2 = 0 end,
       jsonb_build_object('n', i, 'tags', jsonb_build_array('a', 'b')),
       json_build_object('n', i),
       case when i % 10 = 0 then 'note ' || i end,
       timestamptz '2026-09-01 00:00:00+00' + i * interval '1 minute',
       case when i % 100 = 0 then timestamptz '2026-09-20 12:00:00+00' end
from generate_series(1, 6000) i;

insert into t_order_item (order_id, line_no, sku_code, qty)
select o, l, 'sku_' || (o + l) % 20, 1 + o % 3
from generate_series(1, 200) o, generate_series(1, 2) l;

insert into t_sku (code, barcode, title, price)
select 'sku_' || i,
       case when i % 4 = 0 then null else '69' || lpad(i::text, 11, '0') end,
       'SKU ' || i,
       case when i % 5 = 0 then null else i * 10 end
from generate_series(0, 19) i;

insert into t_log (logged_at, msg) values
    ('2026-09-21 10:00:00+00', 'plain'),
    ('2026-09-21 10:01:00+00', E'line one\nline two\tafter tab \x1b[31mred\x1b[0m end'),
    ('2026-09-21 10:02:00+00', null);

insert into t_event (id, occurred_at, kind) values
    (1, '2025-06-01 00:00:00+00', 'signup'),
    (2, '2025-12-31 23:59:59+00', 'login'),
    (3, '2026-02-01 08:00:00+00', 'login');

insert into agentable.agent (name) values ('planner'), ('coder'), ('tester');
insert into agentable.agent_version (agent_id, version, released) values
    (1, 1, true), (1, 2, false), (2, 1, true), (3, 1, false);
insert into agentable.goal (agent_id, title) values
    (1, 'ship m1'), (2, 'write tests'), (null, 'unassigned');

refresh materialized view mv_order_by_status;
analyze;
