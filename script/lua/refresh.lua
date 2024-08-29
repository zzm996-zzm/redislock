
if redis.call("get",KEYS[1]) == ARGV[1] then
    return redis.call("pexpire",KEYS[1],ARGV[2])
else
    -- 返回 0 代表的是 key 不在，或者值不对
    return 0
end