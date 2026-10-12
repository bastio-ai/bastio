-- Transactional accounting: reservations prevent concurrent double-spending.
CREATE TABLE workspace_spend (
 id UUID PRIMARY KEY DEFAULT uuidv7(), customer_id UUID NOT NULL,
 user_id TEXT NOT NULL, amount_cents DOUBLE PRECISION NOT NULL CHECK(amount_cents >= 0),
 state TEXT NOT NULL CHECK(state IN ('reserved','settled')), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX workspace_spend_month ON workspace_spend(customer_id,created_at);
INSERT INTO workspace_spend(customer_id,user_id,amount_cents,state,created_at)
SELECT m.customer_id,c.user_id,SUM(m.cost_cents_exact),'settled',date_trunc('month',m.created_at)
FROM workspace_messages m JOIN workspace_conversations c ON c.id=m.conversation_id AND c.customer_id=m.customer_id
WHERE m.role='assistant' GROUP BY m.customer_id,c.user_id,date_trunc('month',m.created_at);
