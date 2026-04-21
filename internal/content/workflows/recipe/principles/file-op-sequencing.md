# file-op-sequencing

Reads precede edits. Writes create files; edits modify them. Following this sequence keeps edits operating on current bytes and keeps your model of the mount consistent with what's actually there.

## Read-before-Edit

The Edit tool reads the file's current contents internally and refuses to run unless you have already issued a Read call against that path in the current session. That enforcement exists because an edit applied to bytes that differ from your mental model produces silent corruption or a failed match that costs a round-trip to diagnose.

Order for any path you plan to modify:

1. Read the file first.
2. Issue the Edit (or sequence of Edits) using the exact bytes Read returned.

## Batch your reads before the first Edit

When your plan touches N files, Read all N before the first Edit. Batching up front has two benefits: it surfaces "this file doesn't exist yet — I need Write, not Edit" before you start mutating anything, and it lets you compose the whole edit set in one pass rather than interleaving read-edit-read-edit (which is slower AND produces inconsistent intermediate states on the mount).

Pattern:

```
Read A ; Read B ; Read C ; ... ; Edit A ; Edit B ; Write D ; Edit C
```

Not:

```
Read A ; Edit A ; Read B ; Edit B ; Read C ; Edit C
```

## Write creates, Edit modifies

Write is for a path that does not yet exist, or for a complete rewrite. Edit is for a targeted modification of an existing file.

A path that exists but needs substantial restructuring is still a candidate for Edit (multiple targeted replacements) UNLESS the new content shares almost no bytes with the old — then Write is clearer.
