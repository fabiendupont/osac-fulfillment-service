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

-- Create the compute_instance_classes tables:
--
-- This migration establishes the database schema for ComputeInstanceClass resources following the generic schema
-- pattern. ComputeInstanceClass describes a provider-defined SKU or catalog entry for compute offerings. It
-- represents a specific compute offering that tenants can order (e.g., "bm-large" for a bare metal server with
-- 64 cores and 256 GiB of memory).
--
-- The data column stores:
-- - title: Human-friendly short description
-- - description: Human-friendly long description (Markdown)
-- - backend: Provisioning type discriminator (e.g., "baremetal", "virtual")
-- - capabilities: ComputeInstanceClassCapabilities (cores, memory, gpus, storage)
-- - templates: Repeated ComputeInstanceClassTemplateRef (name, site)
-- - status: ComputeInstanceClassStatus (state, message)
-- as JSONB.
--
create table compute_instance_classes (
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

create table archived_compute_instance_classes (
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

-- Add indexes on the name column for fast lookups:
create index compute_instance_classes_by_name on compute_instance_classes (name);

-- Add indexes on the creators column for owner-based queries:
create index compute_instance_classes_by_owner on compute_instance_classes using gin (creators);

-- Add indexes on the tenants column for tenant isolation:
create index compute_instance_classes_by_tenant on compute_instance_classes using gin (tenants);

-- Add indexes on the labels column for label-based queries:
create index compute_instance_classes_by_label on compute_instance_classes using gin (labels);
