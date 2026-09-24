# src/db — local data cache (FG28 placeholder)

This folder will own the tablet-side offline cache introduced in **FG28–FG30**:

```text
Store information
Employee list
Employee images
Display playlist
Display settings
Content
Device configuration
```

Design notes agreed in FG1 so later feature groups stay consistent:

1. One module per concern, no component imports a storage library directly —
   screens read through a repository/hook in `src/features/*`.
2. The cache is keyed by `store_id` (and `device_id` when a device credential
   exists) so a tablet that is re-paired to another store can never mix data.
3. Cache records carry `updated_at` from the server so the display can show how
   old the offline snapshot is.
4. Realtime events (FG22) invalidate a single cache entry instead of refetching
   everything; a full bootstrap happens on reconnect (FG26/FG30).
5. Storage engine decision (IndexedDB via `idb` vs. Cache Storage only) is made
   in FG28 when the data shapes are final.

Until FG28 the display shell (FG10) fetches through `src/services/*` and shows the
connection state from `src/features/health`.
