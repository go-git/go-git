package util

// ValidOFSDeltaBase reports whether an OFS-delta's encoded negative
// offset satisfies the canonical-Git invariant: the resolved base offset
// (deltaOffset - negativeOffset) must be strictly positive (past the
// 12-byte pack header) and strictly less than deltaOffset, since an
// OFS-delta can only reference an earlier entry in the same pack.
//
// Mirrors canonical Git's predicate in packfile.c[1]:
//
//	base_offset = delta_obj_offset - base_offset;
//	if (base_offset <= 0 || base_offset >= delta_obj_offset)
//		return 0;  /* out of bound */
//
// [1]: https://github.com/git/git/blob/v2.54.0/packfile.c#L1289-L1290
func ValidOFSDeltaBase(deltaOffset, negativeOffset int64) bool {
	return negativeOffset > 0 && negativeOffset < deltaOffset
}
