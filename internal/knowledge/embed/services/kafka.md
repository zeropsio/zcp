# Kafka on Zerops

## Keywords
kafka, message queue, event streaming, broker, sasl, topic, partition, event sourcing, pub-sub

## TL;DR
Kafka on Zerops uses SASL PLAIN auth on port 9092, runs 3 brokers in HA mode with replication factor 3, and retains topic data indefinitely (no time/size limits).

## Zerops-Specific Behavior
- Port: **9092** (data broker, SASL PLAIN)
- Schema Registry: **8081** (if enabled)
- Auth: SASL PLAIN — `user` and `password` env vars (auto-generated)
- Bootstrap: `${hostname}:9092`
- Specific brokers: `node-stable-1.db.${hostname}.zerops:9092,...`
- Resources: Up to 40GB RAM, 250GB persistent storage
- **Topic retention: Indefinite** (no time or size limits by default)

## HA Mode (3 brokers)
- 6 partitions across cluster
- Replication factor: 3 (each broker has a copy)
- Default topic replication: 3 (user-overridable per topic)
- Automatic repair on broker failure

## NON_HA Mode
- 1 broker, 3 partitions
- No replication — data loss risk in production

## Configuration
```yaml
# import.yaml
services:
  - hostname: events
    type: kafka@3.8
    mode: HA
```

## Connection
```
bootstrap.servers=${hostname}:9092
security.protocol=SASL_PLAINTEXT
sasl.mechanism=PLAIN
sasl.jaas.config=org.apache.kafka.common.security.plain.PlainLoginModule required username="${user}" password="${password}";
```

## Gotchas
1. **SASL only**: No anonymous connections — always use the generated credentials
2. **Single-node = no replication**: 1 broker with 3 partitions but zero redundancy — never use for production
3. **Indefinite retention**: Topics grow without limit — implement application-level cleanup if needed
4. **250GB storage cap**: Plan topic retention and compaction to stay within limits

## See Also
- zerops://decisions/choose-queue
- zerops://services/nats
- zerops://services/_common-database
