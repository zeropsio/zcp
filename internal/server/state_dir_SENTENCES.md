# Where a zcp keeps the project's records (`server.go`)

- SD-ONE — Every zcp in a container keeps the project's records in the container's one state directory, whatever directory its agent started it in; outside a container they are under the working directory. Breaks: a crewmate started in its copy of the code reads no records and is told to adopt the services the Mate set up.
