-- Seed for the Oracle integration tests (§8.11). gvenzl/oracle-free runs it
-- as SYS on the first start of an empty volume; APP_USER sqlmux is made
-- before it. Values are fixed, so assertions stay stable.
alter session set container = FREEPDB1;
alter session set current_schema = SQLMUX;

create table t_user (
    id         number(10) primary key,
    name       varchar2(100) not null,
    email      varchar2(200),
    active     number(1) default 1 not null,
    created_at date default date '2026-01-01' not null
);
insert into t_user values (1, 'alice', 'alice@example.com', 1, timestamp '2026-01-01 09:00:00');
insert into t_user values (2, 'bob', null, 1, timestamp '2026-01-02 10:30:00');
insert into t_user values (3, 'carol', 'carol@example.com', 0, timestamp '2026-01-03 11:45:15');
insert into t_user values (4, 'dave', 'dave@example.com', 1, timestamp '2026-01-04 12:00:00');
insert into t_user values (5, unistr('\00e9 \d83d\dc4d'), null, 1, timestamp '2026-01-05 13:00:00');

-- over 5000 rows, for paging and counting; one column per type the grid shows
create table t_order (
    id         number(10) primary key,
    user_id    number(10) not null references t_user (id),
    status     varchar2(10) default 'pending' not null check (status in ('pending', 'running', 'done', 'failed')),
    amount     number(10, 2) not null,
    ratio      binary_double,
    note       clob,
    raw_bytes  raw(4),
    created_at timestamp(3) not null,
    due_at     timestamp(3) with time zone
);
insert into t_order
select level, mod(level - 1, 5) + 1,
       decode(mod(level - 1, 4), 0, 'pending', 1, 'running', 2, 'done', 'failed'),
       mod(level - 1, 1000) / 10 + 0.25,
       mod(level - 1, 7) / 4,
       case when mod(level - 1, 10) = 0 then 'tab' || chr(9) || 'and' || chr(10) || 'newline''s' end,
       case when mod(level - 1, 3) = 0 then hextoraw('00FF') end,
       timestamp '2026-01-01 00:00:00.000' + numtodsinterval(level - 1, 'SECOND'),
       case when mod(level - 1, 2) = 0 then timestamp '2026-02-01 08:00:00.000 +02:00' end
from dual connect by level <= 5200;

-- a key of two columns
create table t_order_item (
    order_id number(10) not null references t_order (id),
    line_no  number(3) not null,
    qty      number(5) not null,
    primary key (order_id, line_no)
);
insert into t_order_item values (1, 1, 2);
insert into t_order_item values (1, 2, 1);
insert into t_order_item values (2, 1, 5);
create index ix_order_item_qty on t_order_item (qty);

-- no key: nothing names one row, so no edit (§10.1)
create table t_log (at timestamp, line varchar2(100));
insert into t_log values (timestamp '2026-01-01 00:00:00', 'one');
insert into t_log values (timestamp '2026-01-01 00:00:01', 'two');

create view v_active as select id, name from t_user where active = 1;
create sequence seq_order start with 6000;

create or replace function double_it (x number) return number as
begin
  return x * 2;
end;
/

create or replace procedure touch_user (p_id number) as
begin
  update t_user set name = name where id = p_id;
end;
/

commit;

-- a second schema, with a table of its own
create user sqlmux_other identified by sqlmux quota unlimited on users;
create table sqlmux_other.t_note (id number primary key, body varchar2(100));
insert into sqlmux_other.t_note values (1, 'hello');
grant select, insert, update, delete on sqlmux_other.t_note to sqlmux;
commit;
