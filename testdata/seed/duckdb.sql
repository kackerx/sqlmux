-- Seed for the DuckDB tests (M24 F24.2), the cases of pg.sql in DuckDB's
-- terms. The tests run it into a file of their own; there is no server to
-- load it into. Values are fixed, not now() or random(), so assertions stay
-- stable. A foreign key is written in its create table: DuckDB has no ALTER
-- TABLE ... ADD CONSTRAINT FOREIGN KEY, nor one across schemas.

create schema agentable;

-- in sort order: DuckDB's enum labels go by it
create type order_status as enum ('pending', 'queued', 'running', 'done', 'failed');

create sequence t_user_id;
create table t_user (
    id         bigint primary key default nextval('t_user_id'),
    name       varchar not null,
    email      varchar,
    active     boolean not null default true,
    created_at timestamptz not null default now()
);

-- over 5000 rows, for paging and counting; one column per kind of value
create sequence t_order_id;
create table t_order (
    id         bigint primary key default nextval('t_order_id'),
    user_id    bigint not null references t_user (id),
    status     order_status not null default 'pending',
    amount     decimal(10, 2) not null,
    paid       boolean,
    meta       json,
    raw        blob,
    note       varchar,
    created_at timestamptz not null default now(),
    deleted_at timestamptz
);

-- composite primary key
create table t_order_item (
    order_id bigint not null references t_order (id),
    line_no  int not null,
    sku_code varchar not null,
    qty      int not null default 1,
    primary key (order_id, line_no)
);

-- no primary key, only unique constraints and indexes: code and (title, code)
-- can identify a row, the nullable and expression ones can't (§8.4)
create table t_sku (
    code    varchar not null unique,
    barcode varchar unique,
    title   varchar not null,
    price   decimal(10, 2)
);
create unique index t_sku_title_code on t_sku (title, code);
create unique index t_sku_lower_title on t_sku (lower(title));

-- no primary key and no unique constraint; one row carries control characters (§7.6)
create table t_log (
    logged_at timestamptz not null,
    msg       varchar
);

create view v_paid_order as
    select id, user_id, amount from t_order where paid;

-- the types DuckDB has that the others do not, and a generated column
create table t_types (
    id     int primary key,
    tags   int[],
    st     struct(a int, b varchar),
    mp     map(varchar, int),
    ts_s   timestamp,
    u      uuid,
    iv     interval,
    h      hugeint,
    b      blob,
    area   int generated always as (id * 2) virtual
);

create table agentable.agent (
    id   int primary key,
    name varchar not null unique
);

create table agentable.agent_version (
    agent_id int not null references agentable.agent (id),
    version  int not null,
    released boolean not null default false,
    primary key (agent_id, version)
);

create table agentable.goal (
    id       int primary key,
    agent_id int references agentable.agent (id),
    title    varchar not null
);

insert into t_user (name, email, active, created_at)
select 'user_' || i,
       case when i % 5 = 0 then null else 'user_' || i || '@example.com' end,
       i % 7 <> 0,
       timestamptz '2026-01-01 00:00:00+00' + i * interval '1 day'
from range(1, 51) t(i);

insert into t_order (user_id, status, amount, paid, meta, raw, note, created_at, deleted_at)
select 1 + i % 50,
       (['pending', 'running', 'done', 'failed'])[1 + i % 4]::order_status,
       (i % 1000) + 0.99,
       case when i % 3 = 0 then null else i % 2 = 0 end,
       json_object('n', i, 'tags', json_array('a', 'b')),
       case when i = 1 then '\xDE\xAD\xBE\xEF\x00\xFF'::blob end,
       case when i % 10 = 0 then 'note ' || i end,
       timestamptz '2026-09-01 00:00:00+00' + i * interval '1 minute',
       case when i % 100 = 0 then timestamptz '2026-09-20 12:00:00+00' end
from range(1, 6001) t(i);

insert into t_order_item (order_id, line_no, sku_code, qty)
select o, l, 'sku_' || (o + l) % 20, 1 + o % 3
from range(1, 201) a(o), range(1, 3) b(l);

insert into t_sku (code, barcode, title, price)
select 'sku_' || i,
       case when i % 4 = 0 then null else '69' || lpad(i::varchar, 11, '0') end,
       'SKU ' || i,
       case when i % 5 = 0 then null else i * 10 end
from range(0, 20) t(i);

insert into t_log (logged_at, msg) values
    ('2026-09-21 10:00:00+00', 'plain'),
    ('2026-09-21 10:01:00+00', E'line one\nline two\tafter tab \x1b[31mred\x1b[0m end'),
    ('2026-09-21 10:02:00+00', null);

insert into t_types (id, tags, st, mp, ts_s, u, iv, h, b) values
    (1, [1, 2], {'a': 1, 'b': 'x'}, map {'k': 1}, '2024-01-02 03:04:05.25', '3077b41a-1111-2222-3333-444455556666',
     interval '1 day 2 hours', 170141183460469231731687303715884105727, '\xAA\xBB'::blob),
    (2, null, null, null, null, null, null, null, null);

insert into agentable.agent (id, name) values (1, 'planner'), (2, 'coder'), (3, 'tester');
insert into agentable.agent_version (agent_id, version, released) values
    (1, 1, true), (1, 2, false), (2, 1, true), (3, 1, false);
insert into agentable.goal (id, agent_id, title) values
    (1, 1, 'ship m1'), (2, 2, 'write tests'), (3, null, 'unassigned');

create index t_order_user_created on t_order (user_id, created_at desc);
create sequence invoice_no start 1000;
create sequence agentable.run_no;
create macro fn_order_total(order_id) as (select amount from t_order where id = order_id);
create macro fn_greet(who) as 'hello ' || who;
create macro agentable.fn_agent_count() as (select count(*) from agentable.agent);
create macro fn_range(n) as table select * from range(n);
