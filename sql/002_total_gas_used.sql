
-- sim_all_sims_gas_used is the total gas used in all simulations
-- sim_total_sim_count is the total number of simulations
alter table sbundle
    add column sim_all_sims_gas_used bigint,
    add column sim_total_sim_count int;
