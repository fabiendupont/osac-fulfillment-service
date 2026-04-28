--
-- Copyright (c) 2026 Red Hat Inc.
--
-- Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with
-- the License. You may obtain a copy of the License at
--
--   http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on
-- an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the
-- specific language governing permissions and limitations under the License.
--

-- Create the compute_instance_groups tables:
--
-- This migration establishes the database schema for ComputeInstanceGroup resources following the generic schema
-- pattern. ComputeInstanceGroup manages a scaled set of identical ComputeInstances with placement semantics.
-- Users create a group by specifying a ComputeInstanceClass and desired replica count. The system creates and
-- manages individual ComputeInstance resources.
--
-- The data column stores:
-- - spec: ComputeInstanceGroupSpec (compute_instance_class, replicas, image_ref, ssh_key_refs, subnet,
--         security_groups, user_data, region, placement_policy)
-- - status: ComputeInstanceGroupStatus (state, ready_replicas, instances, message)
-- as JSONB.
--
create table compute_instance_groups (
  id text not null primary key,
  name text not null default '',
  creation_timestamp timestamp with time zone not null default now(),
  deletion_timestamp timestamp with time zone not null default 'epoch',
  finalizers text[] not null default '{}',
  creators text[] not null default '{}',
  tenants text[] not null default '{}',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  version integer not null default 0,
  data jsonb not null
);

create table archived_compute_instance_groups (
  id text not null,
  name text not null default '',
  creation_timestamp timestamp with time zone not null,
  deletion_timestamp timestamp with time zone not null,
  archival_timestamp timestamp with time zone not null default now(),
  creators text[] not null default '{}',
  tenants text[] not null default '{}',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  version integer not null default 0,
  data jsonb not null
);

create index compute_instance_groups_by_name on compute_instance_groups (name);
create index compute_instance_groups_by_owner on compute_instance_groups using gin (creators);
create index compute_instance_groups_by_tenant on compute_instance_groups using gin (tenants);
create index compute_instance_groups_by_label on compute_instance_groups using gin (labels);
