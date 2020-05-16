package constraintstatus

// TODO things to test:
// * deleting the constraint CRD and not recreating the constraint leads to the status
//   object being cleaned up
// * deleting the constraint template leads to the status object being cleaned up
// * deleting the constraint leads to the status object being cleaned up
// * creating the constraint creates the status object
// * the status object gets into the status of the constraint

//func TestTotalConstraintsCache(t *testing.T) {
//	constraintsCache := NewConstraintsCache()
//	if len(constraintsCache.cache) != 0 {
//		t.Errorf("cache: %v, wanted empty cache", spew.Sdump(constraintsCache.cache))
//	}
//
//	constraintsCache.addConstraintKey("test", tags{
//		enforcementAction: util.Deny,
//		status:            metrics.ActiveStatus,
//	})
//	if len(constraintsCache.cache) != 1 {
//		t.Errorf("cache: %v, wanted cache with 1 element", spew.Sdump(constraintsCache.cache))
//	}
//
//	constraintsCache.deleteConstraintKey("test")
//	if len(constraintsCache.cache) != 0 {
//		t.Errorf("cache: %v, wanted empty cache", spew.Sdump(constraintsCache.cache))
//	}
//}
