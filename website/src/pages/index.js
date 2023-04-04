import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import clsx from 'clsx';
import Heading from '@theme/Heading';
import Translate, {translate} from '@docusaurus/Translate';
import styles from './styles.module.css';

function HeroBanner() {
  return (
    <div className={styles.hero} data-theme="dark">
      <div className={styles.heroInner}>
        <Heading as="h1" className={styles.heroProjectTagline}>
          <span
            className={styles.heroTitleTextHtml}
            // eslint-disable-next-line react/no-danger
            dangerouslySetInnerHTML={{
              __html: translate({
                id: 'homepage.hero.title',
                message:
                  'A customizable cloud native policy controller that helps <b>enforce policies and strengthen governance</b>',
                description:
                  'Home page hero title, can contain simple html tags',
              }),
            }}
          />
        </Heading>
        <div className={styles.indexCtas}>
          <Link className="button button--primary" to="/docs">
            <Translate>Get Started</Translate>
          </Link>
          <Link className="button button--info" to="https://docusaurus.new">
            <Translate>Browse the Policy Library</Translate>
          </Link>
          <span className={styles.indexCtasGitHubButtonWrapper}>
            <iframe
              className={styles.indexCtasGitHubButton}
              src="https://ghbtns.com/github-btn.html?user=open-policy-agent&amp;repo=gatekeeper&amp;type=star&amp;count=true&amp;size=large"
              width={160}
              height={30}
              title="GitHub Stars"
            />
          </span>
        </div>
      </div>
    </div>
  );
}

function ContributingCompanies() {
  return (
    <section className={styles.companies}>
      <div className="container">
        <div className="row">
          <div className={clsx('col text--center')}>
            <h2>Contributed by the community in collaboration with</h2>
          </div>
        </div>
        <div className="row">
          <div className={clsx('col col--3 text--center')}>
            <a href="https://cloud.google.com/">
              <img className={styles.companySvg} src="img/google_cloud_logo.svg" />
            </a>
          </div>
          <div className={clsx('col col--3 text--center')}>
            <a href="https://www.microsoft.com/">
              <img className={styles.companySvg} src="img/microsoft_logo.svg" />
            </a>
          </div>
          <div className={clsx('col col--3 text--center')}>
            <a href="https://www.styra.com/">
              <img className={styles.companySvg} src="img/styra_logo-blue.svg" />
            </a>
          </div>
          <div className={clsx('col col--3 text--center')}>
            <a href="https://www.cncf.io/">
              <img className={styles.companySvg} src="img/cncf_logo.svg" />
            </a>
          </div>
        </div>
      </div>
    </section>
  );
}

const FeatureList = [
  {
    title: 'Kubernetes Native',
    Svg: require('@site/static/img/kubernetes_logo.svg').default,
    description: (
      <>
        Gatekeeper makes managing policies on top of Kubernetes easy.
        Policies can be enforced at admission time or at runtime via
        the audit functionality.
      </>
    ),
  },
  {
    title: 'Powered by Open Policy Agent',
    Svg: require('@site/static/img/opa_logo.svg').default,
    description: (
      <>
        Gatekeeper is powered by the Open Policy Agent (OPA) project.
        Using OPA allows you to write policies that are powerful, flexible,
        and portable.
      </>
    ),
  },
  {
    title: 'Extensive Policy Library',
    Svg: require('@site/static/img/library.svg').default,
    description: (
      <>
        Browse the policy library to find existing policies that fit
        your use case. Each policy in the library can be extended and 
        customized to fit your needs.
      </>
    ),
  },
];

function Feature({Svg, title, description}) {
  return (
    <div className={clsx('col col--4')}>
      <div className="text--center">
        <Svg className={styles.featureSvg} role="img" />
      </div>
      <div className="text--center padding-horiz--md">
        <h3>{title}</h3>
        <p>{description}</p>
      </div>
    </div>
  );
}

function HomepageFeatures() {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}

const Services = [
  {
    thumbnail: 'img/azure.png',
    name: 'Azure Kubernetes Service',
    text: 'Azure Policy for Kubernetes is backed by Gatekeeper.',
    website: 'https://learn.microsoft.com/en-us/azure/governance/policy/concepts/policy-for-kubernetes'
  },
  {
    thumbnail: 'img/google_cloud.png',
    name: 'Google Kubernetes Engine',
    text: 'Google Kubernetes Engine Policy Controller is backed by Gatekeeper.',
    website: 'https://cloud.google.com/anthos-config-management/docs/concepts/policy-controller'
  },
  {
    thumbnail: 'img/rancher_logo.png',
    name: 'Rancher',
    text: 'Rancher offers an official Gatekeeper integration as an installable app.',
    website: 'https://ranchermanager.docs.rancher.com/integrations-in-rancher/opa-gatekeeper'
  },
  {
    thumbnail: 'img/aws_logo.png',
    name: 'AWS Elastic Kubernetes Service',
    text: "AWS offers an 'EKS Blueprint' to make installing Gatekeeper easy.",
    website: 'https://aws-quickstart.github.io/cdk-eks-blueprints/addons/opa-gatekeeper/'
  }
]

function ServicesSection() {
  return (
    <div className={clsx(styles.section, styles.sectionAlt)}>
      <div className="container">
        <Heading as="h2" className={clsx('margin-bottom--lg', 'text--center')}>
          Looking for a managed service or integration?
        </Heading>
        <div className="row">
          {Services.map((service) => (
            <div className="col col--4 padding-bottom--lg">
              <a href={service.website}>
                <div className="card" key={service.name}>
                  <div className="card__header">
                    <div className="avatar">
                      <img
                        alt={service.name}
                        src={service.thumbnail}
                        className={styles.serviceLogo}
                      />
                      <div className="avatar__intro padding-top--sm">
                        <div className="avatar__name">{service.name}</div>
                      </div>
                    </div>
                  </div>
                  <p className="card__body">
                    {service.text}
                  </p>
                </div>
              </a>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export default function Home() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={`Hello from ${siteConfig.title}`}
      description="Description will go into a meta tag in <head />">
      <HeroBanner />
      <main>
        <ContributingCompanies />
        <HomepageFeatures />
        <ServicesSection />
      </main>
    </Layout>
  );
}
