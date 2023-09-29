# LoRaWAN Server

To run the file go to the directory and run the following command:

```
cd loraserver/cmd
go build && ./loraserver
```

To run it from dockerfile

```
docker build -t loraserver . && docker run -p 8000:8000 loraserver
```

The loraserver is built in GOlang and the generated binary could be used in any system.
