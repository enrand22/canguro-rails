-- +goose Up
-- Example table. Delete it when the project has its own domain (and delete the
-- matching model, service and tests: code nobody uses is a trap).
CREATE TABLE items (
    id         BIGINT AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(120) NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- UNIQUE de verdad: sin esto, la detección de duplicados (error 1062 de MySQL)
    -- no se puede probar ni ocurrir.
    UNIQUE KEY items_name_unique (name)
) ENGINE = InnoDB;

-- +goose Down
DROP TABLE items;
