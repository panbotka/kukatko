-- 0116_storage_rekey_derived_root_keys: the root objects a storage move's final
-- sweep found, kept with the photo they were found for.
--
-- The final sweep of a storage move (internal/rekey Finish) lists the root
-- thumb/ and hls/ trees and records every moved photo it still finds an object
-- of. The retry pass then re-planned only the keys it knew itself — the
-- registered thumbnail sizes, the recorded renditions' segments — so an object
-- the plan did not know (an old thumbnail size, a segment of an earlier encode)
-- was never moved or deleted: the retry forgot the photo, the next sweep found
-- the same object again, and the move never reached done.
--
-- root_keys is what the sweep found for the photo, so the retry handles exactly
-- that: a key the layout can hold moves under the prefix, anything else in those
-- two regenerable trees is deleted. Empty for a photo recorded by a pass rather
-- than by the sweep.
ALTER TABLE storage_rekey_derived_failures ADD COLUMN IF NOT EXISTS root_keys TEXT[] NOT NULL DEFAULT '{}';

COMMENT ON COLUMN storage_rekey_derived_failures.root_keys IS
    'Root thumb/ and hls/ keys the final sweep found for the photo; the retry moves or deletes each.';
