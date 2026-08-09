-- 0051_subjects_life_years: a person's birth and death year, so a gallery can be
-- read as a life rather than as a pile of dates.
--
-- The library spans 1905–2026. Knowing that the person on a 1948 photograph was
-- born in 1925 turns the picture into "she was about 23 here", and the two years
-- together give the person page a header that says who this is in time
-- ("1923–1998", or "*1923" for somebody still alive).
--
-- Both columns are NULLable and default to NULL: almost every subject will never
-- carry them, and an unknown year has to stay unknown — a zero would be a claim.
-- They are plain years, not dates, because that is what anybody actually knows
-- about the people in a family archive; the age shown from them is therefore an
-- approximation and the UI says so ("~23 let").
--
-- They are meant for subjects of type 'person', but no CHECK ties them to the
-- type: a subject's type is editable, and a constraint that fires on an
-- unrelated edit (person → other) would turn a harmless reclassification into a
-- failed save.
--
-- The CHECKs are the clock-independent half of the validation:
--
--   * >= 1800 — a lower bound well below photography itself, so a typo like the
--     year 198 is rejected while every real subject of a photo archive fits.
--   * death >= birth — the only ordering that can ever be true.
--
-- The upper bound ("not in the future") lives in Go (internal/people), not here:
-- it depends on the current year, and a CHECK may only use immutable
-- expressions — now() is not one, and a constraint that silently changes meaning
-- with the calendar would be worse than none.
--
-- No index. These are display fields read together with the subject row itself,
-- never a filter or a sort key.
--
-- This migration is wrapped in a transaction by the runner.

ALTER TABLE subjects
    ADD COLUMN birth_year INTEGER,
    ADD COLUMN death_year INTEGER,
    ADD CONSTRAINT subjects_birth_year_range CHECK (birth_year IS NULL OR birth_year >= 1800),
    ADD CONSTRAINT subjects_death_year_range CHECK (death_year IS NULL OR death_year >= 1800),
    ADD CONSTRAINT subjects_death_after_birth
        CHECK (birth_year IS NULL OR death_year IS NULL OR death_year >= birth_year);
