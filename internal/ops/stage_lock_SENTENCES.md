# Stage writers and probes

- SL-ORDER — Writers of the same stage wait for one another, while different stage services do not block one another.
- SL-PENDING — A stage writer waits for any live build or version deployment of that service, including one a previous writer's wait left running.
- SL-HEALTH — A Review build's declared health path passes only with an HTTP 2xx or 3xx response.
- SL-VERSION — A Review build's health observation is unavailable when its version was not active throughout or the version could not be read.
- SL-PRIVATE — A private part is built only: it needs no public health request.
