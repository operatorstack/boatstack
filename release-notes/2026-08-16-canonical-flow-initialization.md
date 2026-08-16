### Make fresh Flow runs self-initializing

Repository Flow runs now derive installation inputs from the committed project configuration and executing runtime. They use a distinct control-bundle commit suspension, verify its exact Git revision at the effect boundary, and request product delegation only after installation prerequisites are complete.
