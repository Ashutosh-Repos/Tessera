# ASHUTOSH KUMAR
[Bengaluru, Karnataka, India] • [+91 XXXXX XXXXX] • [your.email@example.com] • [linkedin.com/in/yourprofile] • [github.com/yourusername]

---

## PROFESSIONAL SUMMARY
Systems-focused Software Engineer and New College Graduate with 0-18 months of experience in infrastructure engineering and software development. Deeply passionate about distributed storage reliability, Linux system internals, and performance tuning. Demonstrated expertise in writing high-concurrency tools in **Go** and managing containerized applications with **Kubernetes**. Proven ability to debug low-level systems, build robust automation pipelines, and implement production-grade logging and monitoring. Eager to bring high accountability, a growth mindset, and analytical rigor to Apple Cloud's File/Block Storage Infrastructure team.

---

## TECHNICAL SKILLS
*   **Programming Languages**: Go (Preferred), C, C++, Python, SQL, Bash scripting
*   **Distributed Systems & Storage**: Distributed consensus (Raft), consistent hashing, WAL (Write-Ahead Logging), data replication, file/block I/O, page cache, disk performance profiling
*   **Infrastructure & Orchestration**: Kubernetes (StatefulSets, PV/PVC, Operators), Docker, virtualization, provisioning, system configuration management
*   **Traffic Management & Networking**: Content Delivery Networks (CDN), DNS routing, TCP/UDP sockets, load balancing, proxy servers, HTTP/2
*   **Observability & Reliability**: Prometheus, Grafana (dashboarding/alerting), ELK Stack, Grafana Loki, Linux tracing tools (`strace`, `lsof`, `iostat`, `fio`)
*   **Core Computer Science**: Data Structures & Algorithms, Concurrency (goroutines, channels, locks), Operating Systems, Systems Design

---

## INFRASTRUCTURE & SYSTEMS PROJECTS

### Distributed File/Block Storage Simulator & Replication Engine | *Go, gRPC, Linux I/O*
*Implemented a distributed replica system designed to emulate distributed storage reliability and block-level access.*
*   **Block Layer & Storage Tuning**: Emulated virtual block volumes with 4KB block-alignment, performing benchmarking using `fio` and analyzing disk queue depths to optimize sequential read/write throughput.
*   **Consensus & Consistency**: Developed a Raft-based state machine in **Go** for log replication across 3 simulated nodes, ensuring zero data corruption during simulated node dropouts.
*   **Error Handling & Reliability**: Built recovery routines to read/write from disk utilizing write-ahead logging (WAL) and memory-mapped files (`mmap`), guaranteeing crash consistency.
*   **Monitoring & Observability**: Instrumented node processes with Prometheus metrics tracking read/write latency, replication lag, and sector allocation failures.

### High-Performance CDN Traffic Manager & Load Balancer | *Go, SSE, Redis, Docker*
*A Content Delivery Network reverse proxy and routing engine to manage geographic distribution of requests.*
*   **Traffic Management & Security**: Built a custom load balancer in **Go** implementing consistent hashing and least-connections routing algorithms; integrated a Token Bucket **rate limiter** at the edge for DDoS mitigation.
*   **Auto-healing & Failover**: Designed active background health checks that monitor node socket responsiveness, automatically removing failing instances in `< 100ms` and executing routing failovers with zero downtime.
*   **Caching & Concurrency**: Implemented a thread-safe LRU cache with **Single-Flight** stampede protection, merging duplicate concurrent misses to reduce origin read operations by **40%**.
*   **Telemetry & Observability**: Built a low-latency Server-Sent Events (SSE) log-telemetry stream to pipe transaction latencies, cache statuses, and traffic routes to an interactive control console.

---

## PROFESSIONAL EXPERIENCE (0 - 18 MONTHS / INTERNSHIPS)

### Software & Infrastructure Engineering Intern | *[Company/Organization Name]*
*Bengaluru, India* | *[Month, Year] – Present (or Month, Year)*
*   **Software Development**: Authored and optimized high-performance backend tools in **Go**, utilizing worker pools and channels to parse high-volume system logs concurrently.
*   **System Configuration Management**: Managed cluster configurations and system settings using declarative code, maintaining uniformity across development, testing, and production servers.
*   **Infrastructure Provisioning**: Simulated automated provisioning of virtual machines and storage volumes using virtualization concepts, reducing developer setup latency by **50%**.
*   **Deployment & CI/CD**: Maintained GitOps workflows and CI/CD pipelines, automating containerization with **Docker** and deployment onto **Kubernetes** development namespaces.
*   **Linux Troubleshooting**: Tracked and debugged kernel/user space bottleneck issues using standard tools (`strace`, `lsof`, `tcpdump`), optimizing memory allocation and preventing goroutine leaks.
*   **Teamwork & Growth Mindset**: Participated in daily standups and design reviews, taking full ownership of reliability features and code simplicity to improve software reuse.

---

## EDUCATION

### Bachelor of Technology (B.Tech) in Computer Science & Engineering
*[Your University Name], Bengaluru, India* | *Graduation: [Month, Year]*
*   **GPA / Percentage**: [e.g., 9.2/10 or 88%]
*   **Relevant Coursework**: Operating Systems (Kernel space, IPC, virtual memory), Computer Networks (TCP/IP stack, socket programming), Distributed Systems, Analysis of Algorithms.
*   **Academic Highlights**: [e.g., Developed a custom user-space file system (FUSE) as a final year project]
