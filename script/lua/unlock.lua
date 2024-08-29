
-- 检测是不是预期中的值，如果是预期中的值则可以删除key
if redis.call("get",KEYS[1]) == ARGV[1] then
    return redis.call("del",KEYS[1])
else
    -- 返回 0 代表的是 key 不在，或者值不对
    return 0
end