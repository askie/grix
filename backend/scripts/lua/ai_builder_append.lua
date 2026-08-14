-- ai_builder_append.lua
-- KEYS[1] = ai:builder:{msg_id}
-- ARGV[1] = delta_content
-- ARGV[2] = ttl_seconds (600)

redis.call('APPEND', KEYS[1], ARGV[1])
if redis.call('TTL', KEYS[1]) == -1 then
    redis.call('EXPIRE', KEYS[1], ARGV[2])
end
return redis.call('STRLEN', KEYS[1])
