-- Migrate existing compute_instance_templates to compute_instance_classes.
-- Each template becomes a class with the template's title and description.
-- The template's backend field determines the class backend.
-- A reference from the class back to the template is stored in the templates array.

INSERT INTO compute_instance_classes (id, name, creation_timestamp, labels, annotations, data)
SELECT
    gen_random_uuid()::TEXT,
    t.name,
    NOW(),
    t.labels,
    t.annotations,
    jsonb_build_object(
        'title', t.data->>'title',
        'description', t.data->>'description',
        'backend', COALESCE(t.data->>'backend', 'virtual'),
        'status', jsonb_build_object('state', 2),
        'templates', jsonb_build_array(
            jsonb_build_object(
                'name', t.id,
                'site', COALESCE(t.data->>'site', '')
            )
        )
    )
FROM compute_instance_templates t
WHERE t.deletion_timestamp IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM compute_instance_classes c
      WHERE c.name = t.name AND c.deletion_timestamp IS NULL
  );

-- Update existing compute_instances to reference the class derived from their template.
-- The original template and template_parameters fields are preserved in data for backward compatibility.

UPDATE compute_instances ci
SET data = ci.data || jsonb_build_object(
    'spec', (ci.data->'spec') || jsonb_build_object(
        'compute_instance_class',
        (SELECT c.id FROM compute_instance_classes c
         WHERE c.name = (
             SELECT t.name FROM compute_instance_templates t
             WHERE t.id = ci.data->'spec'->>'template'
             AND t.deletion_timestamp IS NULL
             LIMIT 1
         )
         AND c.deletion_timestamp IS NULL
         LIMIT 1)
    )
)
WHERE ci.deletion_timestamp IS NULL
  AND ci.data->'spec'->>'template' IS NOT NULL
  AND ci.data->'spec'->>'template' != ''
  AND (ci.data->'spec'->>'compute_instance_class' IS NULL
       OR ci.data->'spec'->>'compute_instance_class' = '');
