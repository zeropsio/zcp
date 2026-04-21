# Close — completion predicate

Close is complete when every substep the server returned is attested. For a showcase recipe that is `code-review` + `close-browser-walk`; for a minimal recipe that is `code-review` alone. Read `zerops_workflow action=status` to confirm the pending substep list before attempting step-complete.

## Attestation

```
zerops_workflow action="complete" step="close" attestation="Code review + browser walk complete; fixes applied and re-verified on dev+stage"
```

## Post-completion

The response includes `postCompletion.nextSteps[]` with the export and publish CLI commands. Those commands are reference material — relay them to the user only when the user explicitly asks to archive locally or open a publish PR. The workflow is done at close; export and publish are post-workflow operations the user owns.
