package mutation

// SchemaDB is a database that caches all the implied schemas.
// Will return an error when adding a mutator conflicting with the existing ones.
type SchemaDB struct{}

// Upsert tries to insert or update the given mutator.
// If a conflict is detected, Upsert will return an error
func (s *SchemaDB) Upsert(mutator MutatorWithSchema) error {
	return nil
}

// Remove removes the mutator with the given id from the
// schemadb.
func (s *SchemaDB) Remove(id ID) error {
	return nil
}
