-- Live Activity push-to-start token, reported by the iOS app through
-- POST /v1/devices/bind. Nullable: only iOS devices running a build with the
-- GrixActivity extension, and only while the user keeps Live Activities enabled
-- in iOS Settings, ever produce one. A NULL/empty value means the device is not
-- eligible for a push-to-start and is skipped by the live_activity push fan-out.
ALTER TABLE devices ADD COLUMN IF NOT EXISTS live_activity_token VARCHAR(512);
