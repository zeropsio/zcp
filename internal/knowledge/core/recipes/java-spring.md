# Java Spring Boot on Zerops

Spring Boot with embedded Tomcat. Requires bind address configuration for Zerops networking.

## application.properties (REQUIRED)
```properties
server.address=0.0.0.0
```

## Gotchas
- **server.address=0.0.0.0** is MANDATORY (not localhost/127.0.0.1) for Zerops internal routing
- Without this, service is not accessible within Zerops VXLAN network
- Standard Spring Boot otherwise (no other Zerops-specific changes needed)
