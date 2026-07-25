-- DOM-1: CronJob spec.timeZone was ignored, so next-run countdowns and
-- missed-run detection were evaluated in the server's zone instead of the zone
-- the Kubernetes controller uses. Persist the zone so both agree with reality.
-- NULL means the CronJob declares no spec.timeZone (evaluate in server-local).
ALTER TABLE cronjobs ADD COLUMN time_zone TEXT;
