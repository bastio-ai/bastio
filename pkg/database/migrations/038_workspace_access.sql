CREATE TABLE workspace_access_groups (
 id UUID PRIMARY KEY DEFAULT uuidv7(), customer_id UUID NOT NULL, name TEXT NOT NULL,
 UNIQUE(customer_id,name)
);
CREATE TABLE workspace_access_group_members (
 customer_id UUID NOT NULL, group_id UUID NOT NULL REFERENCES workspace_access_groups(id) ON DELETE RESTRICT,
 user_id TEXT NOT NULL, PRIMARY KEY(customer_id,group_id,user_id)
);
CREATE TABLE workspace_resource_access (
 customer_id UUID NOT NULL, resource_type TEXT NOT NULL CHECK(resource_type IN ('knowledge','assistant')),
 resource_id UUID NOT NULL, group_id UUID NOT NULL REFERENCES workspace_access_groups(id) ON DELETE RESTRICT,
 PRIMARY KEY(customer_id,resource_type,resource_id,group_id)
);
