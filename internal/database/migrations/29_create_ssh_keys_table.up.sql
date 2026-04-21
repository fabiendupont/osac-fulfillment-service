--
-- Copyright (c) 2025 Red Hat Inc.
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

-- Create the ssh_keys tables:
--
-- This migration establishes the database schema for SSHKey resources following the generic schema pattern.
-- SSHKey represents an SSH public key owned by a tenant user. A single SSHKey can be referenced by multiple
-- ComputeInstances, enabling centralized key management.
--
-- The data column stores:
-- - public_key: SSH public key in authorized_keys format
-- - fingerprint: SHA256 fingerprint of the public key (computed by the server)
-- as JSONB.
--
create table ssh_keys (
  id text not null primary key,
  name text not null default '',
  creation_timestamp timestamp with time zone not null default now(),
  deletion_timestamp timestamp with time zone not null default 'epoch',
  finalizers text[] not null default '{}',
  creators text[] not null default '{}',
  tenants text[] not null default '{}',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  version bigint not null default 0,
  data jsonb not null
);

create table archived_ssh_keys (
  id text not null,
  name text not null default '',
  creation_timestamp timestamp with time zone not null,
  deletion_timestamp timestamp with time zone not null,
  archival_timestamp timestamp with time zone not null default now(),
  creators text[] not null default '{}',
  tenants text[] not null default '{}',
  labels jsonb not null default '{}'::jsonb,
  annotations jsonb not null default '{}'::jsonb,
  data jsonb not null
);

-- Add indexes on the name column for fast lookups:
create index ssh_keys_by_name on ssh_keys (name);

-- Add indexes on the creators column for owner-based queries:
create index ssh_keys_by_owner on ssh_keys using gin (creators);

-- Add indexes on the tenants column for tenant isolation:
create index ssh_keys_by_tenant on ssh_keys using gin (tenants);

-- Add indexes on the labels column for label-based queries:
create index ssh_keys_by_label on ssh_keys using gin (labels);
