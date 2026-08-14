-- token_bucket.lua
-- KEYS[1] = rate:{user_id}:{api_tag}
-- ARGV[1] = capacity (burst)
-- ARGV[2] = rate per second
-- ARGV[3] = current timestamp (seconds, float)
-- ARGV[4] = requested tokens (usually 1)
-- Returns: 1 = allowed, 0 = rejected

local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local data = redis.call('HMGET', key, 'tokens', 'last_ts')
local tokens = tonumber(data[1]) or capacity
local last_ts = tonumber(data[2]) or now

local elapsed = math.max(0, now - last_ts)
tokens = math.min(capacity, tokens + elapsed * rate)

if tokens >= requested then
    tokens = tokens - requested
    redis.call('HMSET', key, 'tokens', tokens, 'last_ts', now)
    redis.call('EXPIRE', key, math.ceil(capacity / rate) * 2)
    return 1
else
    redis.call('HMSET', key, 'tokens', tokens, 'last_ts', now)
    redis.call('EXPIRE', key, math.ceil(capacity / rate) * 2)
    return 0
end
