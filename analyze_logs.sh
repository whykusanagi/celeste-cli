#!/bin/bash

# Summary table for log files: counts ERROR and WARN lines per file
# Exit 1 if any log file has more than 5 ERRORs

error_flag=0
printf "%-30s %10s %10s\n" "Filename" "ERRORs" "WARNs"
printf "%-30s %10s %10s\n" "------------------------------" "----------" "----------"

for log_file in *log; do
  if [[ -f "$log_file" ]]; then
    num_errors=$(grep -c " ERROR" "$log_file" || echo 0) 
    num_warns=$(grep -c " WARN" "$log_file" || echo 0) # Counts lines containing " WARN" (leading space for precision)
    printf "%-30s %10d %10d\n" "$log_file" "$num_errors" "$num_warns"
    
    # Set error flag if this log has more than 5 errors
    if (( num_errors > 5 )); then
      error_flag=1
    fi
  fi
done

# Exit with code 1 if any log file exceeded the 5-error threshold, otherwise 0 for success
if (( error_flag == 1 )); then
  exit 1
else
  exit 0
fi
