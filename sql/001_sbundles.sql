create table sbundle
(
    hash                 bytea PRIMARY KEY,
    signer               bytea                    not null,
    cancelled            boolean                  not null default false,
    allow_matching       boolean                  not null default false,
    received_at          timestamp with time zone NOT NULL DEFAULT now(),
    sim_success          boolean                  not null default false,
    sim_error            text,
    simulated_at         timestamp with time zone,
    sim_eff_gas_price    numeric(28, 18),
    sim_profit           numeric(28, 18),
    sim_refundable_value numeric(28, 18),
    sim_gas_used         bigint,
    body                 jsonb                    NOT NULL,
    body_size            int                      NOT NULL,
    origin_id            varchar(255),
    inserted_at          timestamp with time zone NOT NULL DEFAULT now()
);

create index sbundle_signer_idx on sbundle (signer);
create index sbundle_origin_id_idx on sbundle (origin_id);
create index sbundle_inserted_at_idx on sbundle (inserted_at);

create table sbundle_body
(
    hash         bytea references sbundle (hash) on delete cascade,
    element_hash bytea    not null,
    idx          int      not null,
    -- 1 - tx, 2 - bundle, maybe more in the future
    type         smallint not null,
    unique (hash, idx)
);

create index sbundle_body_hash_idx on sbundle_body (hash);
create index sbundle_body_body_element_hash_idx on sbundle_body (element_hash);

-- sbundle_buidler is used by the builder to query for bundles to build
-- builder is only interested in inclusion part of the bundle and profit that can be made from that bundle
create table sbundle_builder
(
    hash              bytea PRIMARY KEY,
    block             bigint                   NOT NULL,
    max_block         bigint                   NOT NULL,
    cancelled         boolean                  NOT NULL DEFAULT false,
    sim_state_block   bigint,
    sim_eff_gas_price numeric(28, 18),
    sim_profit        numeric(28, 18),
    body              jsonb                    NOT NULL,
    inserted_at       timestamp with time zone NOT NULL DEFAULT now()
);


create index sbundle_builder_block_idx on sbundle_builder (block, max_block);
create index sbundle_builder_sim_eff_gas_price_idx on sbundle_builder (sim_eff_gas_price);
create index sbundle_builder_sim_profit_idx on sbundle_builder (sim_profit);
create index sbundle_builder_inserted_at_idx on sbundle_builder (inserted_at);

create table sbundle_builder_used
(
    block_id bigint references built_blocks (block_id) on delete cascade,
    hash     bytea,
    inserted boolean not null default false,
    unique (block_id, hash)
);

create index sbundle_builder_used_block_hash_idx on sbundle_builder_used (hash);


create table sbundle_hint_history
(
    id bigserial primary key,
    block bigint not null,
    hint jsonb not null,
    inserted_at timestamp with time zone not null default now()
);

create index sbundle_hint_history_block_idx on sbundle_hint_history (block);
create index sbundle_hint_history_inserted_at_idx on sbundle_hint_history (inserted_at);
