# Go Function Parameter Count Detector

Requires Go 1.24 or later. Run these commands in your project's Go module:

```nu
go get -tool github.com/woxQAQ/long-funcparam-detectorgo@latest
go tool long-funcparam-detectorgo .
go tool long-funcparam-detectorgo -output json .
go tool long-funcparam-detectorgo -output csv .
```

The default output is grouped text. JSON and CSV contain flat records sorted by path, line, and column, with fields `path`, `line`, `column`, `name`, `params`, and `parameter_count`. `-group-depth` applies only to text output. With no matches, JSON emits `[]` and CSV emits only the header.
