-- Seed for the ClickHouse integration tests (§8.9). The image loads it on the
-- first start of an empty volume. Values are fixed, so assertions stay stable.

create database if not exists sqlmux;
create database if not exists analytics;

-- a primary key: the row identity of a save, checked by a count first (§10.3)
create table sqlmux.t_user (
    id         UInt64,
    name       String,
    email      Nullable(String),
    active     Bool default true,
    created_at DateTime('UTC') default toDateTime('2026-01-01 00:00:00', 'UTC')
) engine = MergeTree order by id;

insert into sqlmux.t_user values
    (1, 'alice', 'alice@example.com', true, '2026-01-01 09:00:00'),
    (2, 'bob', null, true, '2026-01-02 10:30:00'),
    (3, 'carol', 'carol@example.com', false, '2026-01-03 11:45:15'),
    (4, 'dave', 'dave@example.com', true, '2026-01-04 12:00:00'),
    (5, 'é 👍🏽', null, true, '2026-01-05 13:00:00');

-- over 5000 rows, for paging and counting; one column per type the grid shows
create table sqlmux.t_order (
    id         UInt64,
    user_id    UInt64,
    status     Enum8('pending' = 1, 'running' = 2, 'done' = 3, 'failed' = 4),
    amount     Decimal(10, 2),
    paid       Nullable(Bool),
    ratio      Float64,
    tags       Array(String),
    attrs      Map(String, String),
    ref        UUID,
    region     LowCardinality(String),
    note       Nullable(String),
    created_at DateTime64(3, 'UTC')
) engine = MergeTree primary key (id) order by (id, user_id);

insert into sqlmux.t_order
select
    number + 1,
    number % 5 + 1,
    ['pending', 'running', 'done', 'failed'][number % 4 + 1],
    toDecimal64((number % 1000) / 10 + 0.25, 2),
    if(number % 3 = 0, null, number % 2 = 0),
    (number % 7) / 4,
    if(number % 2 = 0, ['a', 'b'], []),
    map('k', toString(number % 3)),
    toUUID('00000000-0000-0000-0000-' || leftPad(toString(number), 12, '0')),
    ['eu', 'us', 'ap'][number % 3 + 1],
    if(number % 10 = 0, 'tab	and\nnewline\\back', null),
    toDateTime64('2026-01-01 00:00:00', 3, 'UTC') + number
from numbers(5200);

-- no key: nothing names one row, so no edit (§10.1)
create table sqlmux.t_log (at DateTime('UTC'), line String) engine = MergeTree order by tuple();
insert into sqlmux.t_log values ('2026-01-01 00:00:00', 'one'), ('2026-01-01 00:00:01', 'two');

-- an engine without mutations: no edit
create table sqlmux.t_mem (id UInt32, v String) engine = Memory;
insert into sqlmux.t_mem values (1, 'x'), (2, 'y');

create view sqlmux.v_active as select id, name from sqlmux.t_user where active;

create table analytics.events (day Date, kind String, n UInt32) engine = SummingMergeTree order by (day, kind);
insert into analytics.events values ('2026-01-01', 'click', 3), ('2026-01-01', 'view', 10), ('2026-01-02', 'click', 1);

create materialized view analytics.mv_daily engine = SummingMergeTree order by day
as select day, sum(n) as n from analytics.events group by day;

create function sqlmux_double as (x) -> x * 2;
