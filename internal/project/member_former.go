package project

// FormerMemberSQL returns the SQL predicate that holds when the account row
// aliased userAlias is shown in the project projectExpr as a former member: it
// holds no membership there whose identity the project may see (none at all,
// or only a withheld exact-match add — Member.IdentityWithheld). The instance's
// system administrator is no exception: their access to every project is
// implicit, and a project must not learn that the role exists, so one who is
// no member reads exactly like anybody else who is none.
//
// It is the one definition of "the project no longer gets to read this
// account's name": an account that left the project, or that never belonged to
// it (an upload credited before the upload link checked membership), keeps its
// rows — a task it opened, a photo it uploaded, an entry in the audit trail —
// but every reader of those rows shows a neutral "former member" with no name,
// so the project never reads a live name the account may have changed in
// another project since. A withheld membership counts as none, so a blind re-add
// of a former member's guessed address cannot bring the name back and tell the
// admin whose address it was.
//
// The predicate is NULL for a LEFT JOINed account that does not exist (a
// deleted account reads as "unknown", not as "former"); wrap it in COALESCE
// where a boolean column is wanted. Both arguments are SQL expressions (a
// placeholder like "$2" or a column like "t.project_id" for projectExpr, a
// table alias for userAlias); they are pasted verbatim, so they must never
// carry user input.
func FormerMemberSQL(projectExpr, userAlias string) string {
	return "(CASE WHEN " + userAlias + ".uid IS NULL THEN NULL ELSE NOT EXISTS (SELECT 1 FROM project_members fmm" +
		" WHERE fmm.user_uid = " + userAlias + ".uid AND fmm.project_uid = " + projectExpr +
		" AND NOT fmm.identity_withheld) END)"
}

// ShownNameSQL returns the SQL expression a project shows the account row
// aliased userAlias by: its display name, else its username — or the empty
// string when the account is a former member of projectExpr (FormerMemberSQL)
// or does not exist (a LEFT JOINed row that is missing). It pairs with
// FormerMemberSQL, which tells the client which of the two empty names it is.
func ShownNameSQL(projectExpr, userAlias string) string {
	return "CASE WHEN " + FormerMemberSQL(projectExpr, userAlias) + " THEN '' ELSE COALESCE(NULLIF(" +
		userAlias + ".display_name, ''), " + userAlias + ".username, '') END"
}
